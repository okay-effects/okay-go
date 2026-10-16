package okay

import (
	"fmt"
	"time"
)

// remote: a branch's operation, answered by its parent's handlers
func remote(r *Run, t tagged) any {
	rep := make(chan reply, 1)
	select {
	case r.remote <- request{t, rep}:
	case <-r.done:
		panic(stopped{})
	}
	select {
	case a := <-rep:
		if a.cancel {
			panic(stopped{})
		}
		if a.failed != nil {
			panic(a.failed)
		}
		return a.ans
	case <-r.done:
		panic(stopped{})
	}
}

// Done: closed when this program is cancelled (a lost race); never for the top
func (r *Run) Done() <-chan struct{} { return r.done }

// Await: a value from a channel; a cancelled branch stops here, its defers run
func Await[T any](r *Run, ch <-chan T) T {
	select {
	case v := <-ch:
		return v
	case <-r.done:
		panic(stopped{})
	}
}

// Sleep: d, or until cancelled
func Sleep(r *Run, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-r.done:
		panic(stopped{})
	}
}

type finished struct {
	i     int
	value any
	panic any
}

// branches: each in its own goroutine and Run; their outer operations answered
// here, one at a time, by the handlers in force where branches is called;
// wanted says how many results end it (all, or the first)
func branches(r *Run, bs []func(*Run) any, first bool) (int, []any) {
	r.branching.Add(1)
	defer r.branching.Add(-1)
	reqs := make(chan request)
	out := make(chan finished, len(bs))
	runs := make([]*Run, len(bs))
	for i, b := range bs {
		br := &Run{remote: reqs, done: make(chan struct{})}
		runs[i] = br
		go func() {
			defer func() {
				if p := recover(); p != nil {
					if _, ok := p.(stopped); ok {
						out <- finished{i: i, panic: stopped{}}
						return
					}
					out <- finished{i: i, panic: p}
				}
			}()
			out <- finished{i: i, value: b(br)}
		}()
	}
	results := make([]any, len(bs))
	winner, left := -1, len(bs)
	var failure any
	cancel := func(except int) {
		for j, br := range runs {
			if j != except {
				select {
				case <-br.done:
				default:
					close(br.done)
				}
			}
		}
	}
	for left > 0 {
		select {
		case q := <-reqs:
			q.reply <- answerFor(r, q.t)
		case f := <-out:
			left--
			switch {
			case f.panic == nil:
				results[f.i] = f.value
				if first && winner < 0 {
					winner = f.i
					cancel(f.i)
				}
			case isStopped(f.panic):
			default:
				if failure == nil {
					failure = f.panic
					cancel(-1)
				}
			}
		}
	}
	if failure != nil {
		panic(failure)
	}
	return winner, results
}

func isStopped(p any) bool { _, ok := p.(stopped); return ok }

// answerFor: an operation of a branch performed here, by the parent — whose own
// capabilities are its to use again while it answers; a failure of the handler goes back to the branch
func answerFor(r *Run, t tagged) (rep reply) {
	n := r.branching.Swap(0)
	defer r.branching.Store(n)
	defer func() {
		if p := recover(); p != nil {
			if isStopped(p) {
				panic(p)
			}
			rep = reply{failed: p}
		}
	}()
	return reply{ans: perform(r, t)}
}

// All: every branch at once; their values in order. A branch uses the parent's
// capabilities through Lend(branch, c); its own handlers it installs itself. A failure in one cancels the others and is raised here.
func All[A any](r *Run, bs ...func(*Run) A) []A {
	fs := make([]func(*Run) any, len(bs))
	for i, b := range bs {
		fs[i] = func(br *Run) any { return b(br) }
	}
	_, vs := branches(r, fs, false)
	out := make([]A, len(vs))
	for i, v := range vs {
		out[i] = v.(A)
	}
	return out
}

// Par: two branches of different types at once.
func Par[A, B any](r *Run, a func(*Run) A, b func(*Run) B) (A, B) {
	_, vs := branches(r, []func(*Run) any{
		func(br *Run) any { return a(br) },
		func(br *Run) any { return b(br) },
	}, false)
	return vs[0].(A), vs[1].(B)
}

// Race: the first branch to finish wins; the others are cancelled where they wait or perform next, their defers run.
func Race[A any](r *Run, bs ...func(*Run) A) A {
	fs := make([]func(*Run) any, len(bs))
	for i, b := range bs {
		fs[i] = func(br *Run) any { return b(br) }
	}
	w, vs := branches(r, fs, true)
	if w < 0 {
		panic(fmt.Sprintf("okay: Race: no branch finished"))
	}
	return vs[w].(A)
}
