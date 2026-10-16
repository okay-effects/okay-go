package okay

import (
	"errors"
	"testing"
)

type Price struct {
	Answers[float64]
	Sku string
}

func TestComputationStepsThroughTheHost(t *testing.T) {
	c := Start(func(r *Run, h Host) float64 {
		price := Outside[Price](h)
		// a handler inside the program answers its own operations; the host sees only the rest
		return reader(r, 3, func(qty Cap[Ask[int]]) float64 {
			return Perform(price, Price{Sku: "tea"}) * float64(Perform(qty, Ask[int]{}))
		})
	})
	s := c.Step()
	if s.Kind != Yielded || s.Op.(Price).Sku != "tea" {
		t.Fatal(s)
	}
	if s = c.Resume(4.0); s.Kind != Done || s.Value != 12 {
		t.Fatal(s)
	}
}

func TestFailRaisedAtTheOperation(t *testing.T) {
	c := Start(func(r *Run, h Host) (out string) {
		defer func() {
			if p := recover(); p != nil {
				out = "recovered: " + p.(error).Error()
			}
		}()
		Perform(Outside[Price](h), Price{Sku: "x"})
		return "unreached"
	})
	c.Step()
	if s := c.Fail(errors.New("no such sku")); s.Kind != Done || s.Value != "recovered: no such sku" {
		t.Fatal(s)
	}
}

func TestCancelRunsDefers(t *testing.T) {
	cleaned := false
	c := Start(func(r *Run, h Host) int {
		defer func() { cleaned = true }()
		return int(Perform(Outside[Price](h), Price{}))
	})
	c.Step()
	if s := c.Cancel(); s.Kind != Cancelled || !cleaned {
		t.Fatal(s, cleaned)
	}
}

func TestAFailureInTheProgram(t *testing.T) {
	c := Start(func(r *Run, h Host) int { panic("boom") })
	if s := c.Step(); s.Kind != Failed || s.Failure != "boom" {
		t.Fatal(s)
	}
}

func TestAbortingAndReturningAndRaise(t *testing.T) {
	cleaned := false
	got := Main(func(r *Run) string {
		return Returning(r, func(Ask[int]) any { return Abort(-1) }, func(n int) string {
			if n < 0 {
				return "aborted"
			}
			return "done"
		}, func(a Cap[Ask[int]]) int {
			defer func() { cleaned = true }()
			Perform(a, Ask[int]{})
			return 1
		})
	})
	if got != "aborted" || !cleaned {
		t.Fatal(got, cleaned)
	}
	msg := Main(func(r *Run) (out string) {
		return Handle(r, func(Ask[int]) any { return Raise("refused") }, func(a Cap[Ask[int]]) (out string) {
			defer func() { out = recover().(string) }()
			Perform(a, Ask[int]{})
			return ""
		})
	})
	if msg != "refused" {
		t.Fatal(msg)
	}
}
