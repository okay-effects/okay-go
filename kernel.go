package okay

import (
	"fmt"
	"iter"
)

// ---- the kernel's protocol (okay-kernel): a program handed out, stepped by a host ----

// StepKind: where a handed-out program stands.
type StepKind int

const (
	Done      StepKind = iota // it answered: Value
	Yielded                   // it performs Op through a capability the host owns: Resume, Fail or Cancel it
	Failed                    // it failed: Failure
	Cancelled                 // it was cancelled, its defers run
)

// Step: one stop of a handed-out program.
type Step[A any] struct {
	Kind    StepKind
	Value   A
	Op      any
	Failure any
}

// Computation: a program handed out to a host, run one step at a time. The host
// sees only operations and answers — the protocol between implementations.
type Computation[A any] struct {
	next    func() (struct{}, bool)
	stop    func()
	op      any
	answer  any
	raise   any
	value   A
	failure any
	over    bool
}

// Host: the outermost owner of a handed-out program — capabilities whose operations go to the host.
type Host struct {
	run   *Run
	owner *frame
}

// Outside: a capability whose operations the host answers, step by step.
func Outside[O any](h Host) Cap[O] {
	return Cap[O]{run: h.run, owner: h.owner, name: fmt.Sprintf("host %T", *new(O))}
}

// raised: an answer that is a failure, raised at the operation (the kernel's Resume.fail)
type raised struct{ err any }

// Start hands body out: nothing runs until the first Step.
func Start[A any](body func(r *Run, host Host) A) *Computation[A] {
	c := &Computation[A]{}
	next, stop := iter.Pull(func(yield func(struct{}) bool) {
		defer func() {
			if p := recover(); p != nil {
				if !isStopped(p) {
					c.failure = p
				}
			}
		}()
		r := &Run{}
		c.value = Handle(r, func(op any) any {
			c.op = op
			if !yield(struct{}{}) {
				panic(stopped{})
			}
			if c.raise != nil {
				e := c.raise
				c.raise = nil
				return raised{e}
			}
			return c.answer
		}, func(hc Cap[any]) A { return body(r, Host{run: r, owner: hc.owner}) })
	})
	c.next, c.stop = next, stop
	return c
}

func (c *Computation[A]) step() Step[A] {
	if c.over {
		panic("okay: a finished computation stepped again")
	}
	if _, more := c.next(); more {
		return Step[A]{Kind: Yielded, Op: c.op}
	}
	c.over = true
	if c.failure != nil {
		return Step[A]{Kind: Failed, Failure: c.failure}
	}
	return Step[A]{Kind: Done, Value: c.value}
}

// Step: the program run to its value, its failure, or its first operation for the host.
func (c *Computation[A]) Step() Step[A] { return c.step() }

// Resume: the operation answered with v; the program runs to its next stop.
func (c *Computation[A]) Resume(v any) Step[A] { c.answer = v; return c.step() }

// Fail: the operation answered with a failure, raised where it was performed —
// a recover around it in the program sees it.
func (c *Computation[A]) Fail(err any) Step[A] { c.raise = err; return c.step() }

// Cancel: the program stopped where it is, its defers run.
func (c *Computation[A]) Cancel() Step[A] {
	c.stop()
	c.over = true
	return Step[A]{Kind: Cancelled}
}

// ---- the kernel's shapes of handlers, on coroutines ----
// Answering: Answer. Translating: Answer whose function performs through outer
// capabilities. Returning: Returning. Aborting: an answer of Abort(v). A failure
// answered: Raise(err). General — k called many times — is package prog's Control.

// aborted: the handler ends its body here, with this value for the whole handler
type aborted struct{ v any }

// Abort: as an answer, the body is stopped where it performed (its defers run)
// and the handler's value is v — never resumed (the kernel's Aborting).
func Abort(v any) any { return aborted{v} }

// Raise: as an answer, err is raised where the operation was performed.
func Raise(err any) any { return raised{err} }

// Returning: the handler's value made from its body's (the kernel's Returning).
func Returning[O any, A, B any](r *Run, answer func(O) any, ret func(A) B, body func(Cap[O]) A) B {
	return ret(Handle(r, answer, body))
}
