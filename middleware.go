package okay

import (
	"fmt"
	"reflect"
)

// ---- a handed-out program answered in one place: every operation, as data, through middleware

// Answerer: the host's answer to any operation of a handed-out program; an
// error is raised in the program where the operation was performed.
type Answerer func(op any) (any, error)

// Drive: the program run to its value, every operation it performs for the host
// answered by answer. A failure of the program is the error.
func Drive[A any](c *Computation[A], answer Answerer) (A, error) {
	s := c.Step()
	for s.Kind == Yielded {
		if v, err := answer(s.Op); err != nil {
			s = c.Fail(err)
		} else {
			s = c.Resume(v)
		}
	}
	if s.Kind == Failed {
		var zero A
		return zero, fmt.Errorf("okay-go: %v", s.Failure)
	}
	return s.Value, nil
}

// Middleware: around every operation — monitor, log, authorize, record; next answers it.
type Middleware func(op any, next Answerer) (any, error)

// With: answer wrapped in middleware, the first outermost.
func With(answer Answerer, ms ...Middleware) Answerer {
	for i := len(ms) - 1; i >= 0; i-- {
		m, next := ms[i], answer
		answer = func(op any) (any, error) { return m(op, next) }
	}
	return answer
}

// Only: m for the operations of type O alone; the others pass by untouched.
func Only[O any](m func(op O, next Answerer) (any, error)) Middleware {
	return func(op any, next Answerer) (any, error) {
		if o, ok := op.(O); ok {
			return m(o, next)
		}
		return next(op)
	}
}

// Routes: answers by the operation's type.
type Routes map[reflect.Type]Answerer

// Route: f answers the operations of type O.
func Route[O Op[R], R any](rs Routes, f func(O) (R, error)) {
	rs[reflect.TypeFor[O]()] = func(op any) (any, error) { return f(op.(O)) }
}

// Answer: the route of op's type; an operation with none is refused by name.
func (rs Routes) Answer(op any) (any, error) {
	if f, ok := rs[reflect.TypeOf(op)]; ok {
		return f(op)
	}
	return nil, fmt.Errorf("okay-go: no route for %T", op)
}

// Entry: one operation and how it was answered.
type Entry struct {
	Op     any
	Answer any
	Err    error
}

// Record: every operation and its answer appended to log.
func Record(log *[]Entry) Middleware {
	return func(op any, next Answerer) (any, error) {
		v, err := next(op)
		*log = append(*log, Entry{op, v, err})
		return v, err
	}
}

// Replay: a recorded run answered again from its log, nothing reached; an
// operation other than the one recorded is refused, the run having diverged.
func Replay(log []Entry) Answerer {
	i := 0
	return func(op any) (any, error) {
		if i >= len(log) {
			return nil, fmt.Errorf("okay-go replay: %T %+v past the end of the log", op, op)
		}
		e := log[i]
		i++
		if !reflect.DeepEqual(e.Op, op) {
			return nil, fmt.Errorf("okay-go replay: diverged at %d: recorded %+v, now %+v", i-1, e.Op, op)
		}
		return e.Answer, e.Err
	}
}
