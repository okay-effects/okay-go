// Package okay: algebraic effects for Go, in direct style, with capabilities.
//
// A handler makes a capability and hands it to its body; the body performs
// operations through it, as ordinary calls. Each program runs as a coroutine
// (iter.Pull): performing suspends it, the handler that owns the capability
// answers, and the program resumes. A handler answers only the operations of
// ITS capability — by identity, not by type — so two instances of one effect
// coexist; an operation of another passes outward. A capability used after
// its handler returned is refused by name.
package okay

import (
	"fmt"
	"iter"
	"sync/atomic"
)

// Answers[R] marks an operation answering R: embed it in the operation's struct.
type Answers[R any] struct{}

func (Answers[R]) answers(R) {}

// Op[R] is an operation answering R.
type Op[R any] interface{ answers(R) }

// Run is one program's runtime: the chain of its live handlers, innermost first.
// One Run belongs to one goroutine; a parallel branch has its own, its operations
// that no handler of its own owns going to the parent over a channel.
type Run struct {
	branching atomic.Int32 // > 0 while its branches run: its capabilities must be lent to them
	active    *frame
	remote    chan request  // a branch's: where its outer operations go
	done      chan struct{} // closed when the branch is cancelled
}

type request struct {
	t     tagged
	reply chan reply
}

type reply struct {
	ans    any
	failed any // a panic of the parent's handler, raised in the branch
	cancel bool
}

// frame is a handler's coroutine: what its body performed, and where it answers.
type frame struct {
	outer *frame
	yield func(struct{}) bool
	op    tagged
	ans   any
	done  bool
}

type tagged struct {
	owner *frame
	op    any
}

// Cap[O] is the capability to perform the operations O, given by a handler to its body.
type Cap[O any] struct {
	run   *Run
	owner *frame
	name  string
}

func (c Cap[O]) String() string { return fmt.Sprintf("Cap(%s)@%p", c.name, c.owner) }

// Perform: the operation through its capability, answered by the handler that made it.
func Perform[O Op[R], R any](c Cap[O], op O) R {
	if c.run.branching.Load() > 0 {
		panic(fmt.Sprintf("okay: %T through %v while its branches run: inside a branch, use Lend(branch, capability)", op, c))
	}
	return perform(c.run, tagged{c.owner, op}).(R)
}

// Lend: a capability of the parent, for use inside a branch — its operations go to the parent's handler.
func Lend[O any](branch *Run, c Cap[O]) Cap[O] {
	return Cap[O]{run: branch, owner: c.owner, name: c.name}
}

// stopped: the coroutine is being stopped (a consumer broke out); unwinding runs the program's defers
type stopped struct{}

func perform(r *Run, t tagged) any {
	f := r.active
	if f == nil && r.remote != nil {
		return remote(r, t)
	}
	if f == nil {
		if t.owner != nil && t.owner.done {
			panic(fmt.Sprintf("okay: %T via a capability used outside its handler — the capability escaped its scope", t.op))
		}
		panic(fmt.Sprintf("okay: %T performed outside every handler", t.op))
	}
	f.op = t
	if !f.yield(struct{}{}) {
		panic(stopped{})
	}
	return f.ans
}

// Main runs a program and its handlers: the top, where nothing is handled.
func Main[A any](body func(*Run) A) A { return body(&Run{}) }

// Handle makes a capability for O, runs body with it, and answers each of its
// operations with answer. answer runs where the handler is installed, so it may
// perform through capabilities from outside — a component built on another.
func Handle[O any, A any](r *Run, answer func(O) any, body func(Cap[O]) A) A {
	outer := r.active
	me := &frame{outer: outer}
	c := Cap[O]{run: r, owner: me, name: fmt.Sprintf("%T", *new(O))}
	var result A
	next, stop := iter.Pull(func(y func(struct{}) bool) {
		defer func() {
			if p := recover(); p != nil {
				if _, ok := p.(stopped); !ok {
					panic(p)
				}
			}
		}()
		me.yield = y
		result = body(c)
	})
	defer func() { me.done = true; stop(); r.active = outer }()
	for {
		r.active = me
		_, more := next()
		r.active = outer
		if !more {
			return result
		}
		t := me.op
		switch {
		case t.owner == me:
			me.ans = answer(t.op.(O))
		case t.owner.done:
			panic(fmt.Sprintf("okay: %T via a capability used outside its handler — the capability escaped its scope", t.op))
		case outer == nil && r.remote == nil:
			panic(fmt.Sprintf("okay: %T: no live handler owns its capability", t.op))
		default: // not mine: pass it outward, unchanged — to the parent, from a branch
			me.ans = perform(r, t)
		}
	}
}

// Answer is Handle for a typed answer: f answers each operation of O with an R.
func Answer[O Op[R], R any, A any](r *Run, f func(O) R, body func(Cap[O]) A) A {
	return Handle(r, func(o O) any { return f(o) }, body)
}

// Stream: the values a producer emits through its capability, as a Go iterator.
// Range over it; a break stops the producer where it is, its defers run.
func Stream[T any](r *Run, producer func(Cap[Emit[T]])) iter.Seq[T] {
	return func(yield func(T) bool) {
		outer := r.active
		me := &frame{outer: outer}
		c := Cap[Emit[T]]{run: r, owner: me, name: fmt.Sprintf("Emit[%T]", *new(T))}
		next, stop := iter.Pull(func(y func(struct{}) bool) {
			defer func() {
				if p := recover(); p != nil {
					if _, ok := p.(stopped); !ok {
						panic(p)
					}
				}
			}()
			me.yield = y
			producer(c)
		})
		defer func() { me.done = true; stop(); r.active = outer }()
		for {
			r.active = me
			_, more := next()
			r.active = outer
			if !more {
				return
			}
			t := me.op
			if t.owner == me {
				if !yield(t.op.(Emit[T]).Value) {
					return
				}
				me.ans = struct{}{}
			} else {
				me.ans = perform(r, t)
			}
		}
	}
}

// Emit[T]: a value told to a Stream.
type Emit[T any] struct {
	Answers[struct{}]
	Value T
}

// Yield: a value to the stream that owns the capability.
func Yield[T any](c Cap[Emit[T]], v T) { Perform(c, Emit[T]{Value: v}) }
