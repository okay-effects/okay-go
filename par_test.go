package okay

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type Tick struct{ Answers[int] }

func counter[A any](r *Run, body func(Cap[Tick]) A) (A, int) {
	n := 0
	a := Answer(r, func(Tick) int { n++; return n }, body)
	return a, n
}

func TestAllRunsInParallelAndSharesParentHandlers(t *testing.T) {
	start := time.Now()
	got, ticks := 0, 0
	Main(func(r *Run) int {
		got, ticks = counter(r, func(c Cap[Tick]) int {
			vs := All(r, func(br *Run) int {
				Sleep(br, 50*time.Millisecond)
				Perform(Lend(br, c), Tick{})
				return 1
			}, func(br *Run) int {
				Sleep(br, 50*time.Millisecond)
				Perform(Lend(br, c), Tick{})
				return 2
			}, func(br *Run) int {
				// a handler of the branch's own, the parent's beside it
				return reader(br, 40, func(a Cap[Ask[int]]) int { Perform(Lend(br, c), Tick{}); return Perform(a, Ask[int]{}) })
			})
			return vs[0] + vs[1] + vs[2]
		})
		return 0
	})
	if got != 43 || ticks != 3 {
		t.Fatalf("want 43 and 3 ticks, got %d %d", got, ticks)
	}
	if el := time.Since(start); el > 90*time.Millisecond {
		t.Fatalf("branches did not run in parallel: %v", el)
	}
}

func TestParTyped(t *testing.T) {
	var n int
	var s string
	Main(func(r *Run) int {
		n, s = Par(r, func(*Run) int { return 7 }, func(*Run) string { return "seven" })
		return 0
	})
	if n != 7 || s != "seven" {
		t.Fatal(n, s)
	}
}

func TestRaceCancelsTheLoser(t *testing.T) {
	var loserCleaned atomic.Bool
	start := time.Now()
	got := Main(func(r *Run) string {
		return Race(r, func(br *Run) string {
			defer loserCleaned.Store(true)
			Sleep(br, time.Second)
			return "slow"
		}, func(br *Run) string {
			Sleep(br, 20*time.Millisecond)
			return "fast"
		})
	})
	if got != "fast" || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("%s in %v", got, time.Since(start))
	}
	if !loserCleaned.Load() {
		t.Fatal("the loser's defer must run")
	}
}

func TestTimeoutWithAwait(t *testing.T) {
	ch := make(chan int) // never sent
	got := Main(func(r *Run) int {
		return Race(r,
			func(br *Run) int { return Await(br, ch) },
			func(br *Run) int { Sleep(br, 10*time.Millisecond); return -1 })
	})
	if got != -1 {
		t.Fatal(got)
	}
}

func TestFailureInABranchIsRaisedAndCancelsTheOthers(t *testing.T) {
	var cancelled atomic.Bool
	defer func() {
		p := recover()
		if p == nil || !strings.Contains(p.(string), "boom") {
			t.Fatalf("want the branch's failure, got %v", p)
		}
		if !cancelled.Load() {
			t.Fatal("the other branch must be cancelled")
		}
	}()
	Main(func(r *Run) []int {
		return All(r, func(br *Run) int {
			defer cancelled.Store(true)
			Sleep(br, time.Second)
			return 1
		}, func(br *Run) int { panic("boom") })
	})
}

func TestAParentCapabilityUnlentIsRefused(t *testing.T) {
	defer func() {
		p := recover()
		if p == nil || !strings.Contains(p.(string), "use Lend") {
			t.Fatalf("want a refusal naming Lend, got %v", p)
		}
	}()
	Main(func(r *Run) int {
		v, _ := counter(r, func(c Cap[Tick]) int {
			return All(r, func(br *Run) int { return Perform(c, Tick{}) })[0]
		})
		return v
	})
}
