// Package prog: programs as data, for what coroutines cannot do — a continuation
// resumed many times (search, backtracking), kept and resumed later (a pause, a
// replay). The same design as okay's coroutine core: a handler makes a capability,
// operations are answered by its identity. A program is a value; Run walks it in
// a loop, so the stack never grows.
package prog

import "fmt"

// Answers[R] marks an operation answering R: embed it in the operation's struct.
type Answers[R any] struct{}

func (Answers[R]) answers(R) {}

// Op[R] is an operation answering R.
type Op[R any] interface{ answers(R) }

type node interface{}
type pureN struct{ v any }
type performN struct {
	owner *scope
	op    any
}
type bindN struct {
	m node
	f func(any) node
}

// scope is a handler's identity: its capability's operations are its own
type scope struct{ _ byte }

// Prog[A] is a program answering A.
type Prog[A any] struct{ n node }

// Cap[O] is the capability to perform O, made by a handler for its body.
type Cap[O any] struct{ owner *scope }

func Pure[A any](a A) Prog[A] { return Prog[A]{pureN{a}} }

// Perform: an operation through its capability.
func Perform[O Op[R], R any](c Cap[O], op O) Prog[R] { return Prog[R]{performN{c.owner, op}} }

// Then: the program, then f of its value.
func Then[A, B any](p Prog[A], f func(A) Prog[B]) Prog[B] {
	return Prog[B]{bindN{p.n, func(x any) node { return f(x.(A)).n }}}
}

func Map[A, B any](p Prog[A], f func(A) B) Prog[B] {
	return Then(p, func(a A) Prog[B] { return Pure(f(a)) })
}

// Defer: p made when the run gets to it — a tail call in constant stack.
func Defer[A any](p func() Prog[A]) Prog[A] {
	return Prog[A]{bindN{pureN{nil}, func(any) node { return p().n }}}
}

// Control: the general handler. clause gets each operation of its capability and
// k, the rest of the body up to this handler, a function: call it once, many
// times, never, now or later. ret makes the handler's answer of the body's value.
func Control[O Op[R], R, A, B any](ret func(A) Prog[B], clause func(op O, k func(R) Prog[B]) Prog[B],
	body func(Cap[O]) Prog[A]) Prog[B] {
	me := &scope{}
	p := body(Cap[O]{me}).n
	r := func(a any) node { return ret(a.(A)).n }
	cl := func(op any, k func(any) node) node {
		return clause(op.(O), func(x R) Prog[B] { return Prog[B]{k(x)} }).n
	}
	return Prog[B]{handle(me, r, cl, p)}
}

// Answer: each operation answered by f, the body resumed with it.
func Answer[O Op[R], R, A any](f func(O) R, body func(Cap[O]) Prog[A]) Prog[A] {
	return Control(Pure[A], func(op O, k func(R) Prog[A]) Prog[A] { return k(f(op)) }, body)
}

// Into: each operation a program — through capabilities from outside — then the body resumed.
func Into[O Op[R], R, A any](f func(O) Prog[R], body func(Cap[O]) Prog[A]) Prog[A] {
	return Control(Pure[A], func(op O, k func(R) Prog[A]) Prog[A] { return Then(f(op), k) }, body)
}

// handle: a loop over the program's nodes; its own operations to the clause, k
// the rest under this handler again; others passed outward, unchanged
func handle(me *scope, ret func(any) node, clause func(any, func(any) node) node, p node) node {
	for {
		switch n := p.(type) {
		case pureN:
			return ret(n.v)
		case performN:
			return on(me, ret, clause, n, ret)
		case bindN:
			switch m := n.m.(type) {
			case pureN:
				p = n.f(m.v)
			case performN:
				f := n.f
				return on(me, ret, clause, m, func(x any) node { return handle(me, ret, clause, f(x)) })
			case bindN:
				g, f := m.f, n.f
				p = bindN{m.m, func(x any) node { return bindN{g(x), f} }}
			}
		}
	}
}

func on(me *scope, ret func(any) node, clause func(any, func(any) node) node, n performN, k func(any) node) node {
	if n.owner == me {
		return clause(n.op, k)
	}
	return bindN{n, k}
}

// Run: the program's value, every operation handled; a loop.
func Run[A any](p Prog[A]) A {
	n := p.n
	for {
		switch x := n.(type) {
		case pureN:
			return x.v.(A)
		case performN:
			refuse(x)
		case bindN:
			switch m := x.m.(type) {
			case pureN:
				n = x.f(m.v)
			case performN:
				refuse(m)
			case bindN:
				g, f := m.f, x.f
				n = bindN{m.m, func(v any) node { return bindN{g(v), f} }}
			}
		}
	}
}

func refuse(p performN) {
	panic(fmt.Sprintf("prog: %T via a capability no live handler owns — the capability escaped its scope, or was never handled", p.op))
}
