package okay

import (
	"strings"
	"testing"
)

type Ask[T any] struct{ Answers[T] }

func reader[T any, A any](r *Run, v T, body func(Cap[Ask[T]]) A) A {
	return Answer(r, func(Ask[T]) T { return v }, body)
}

func TestTwoInstancesOfOneEffect(t *testing.T) {
	got := Main(func(r *Run) int {
		return reader(r, 1, func(a Cap[Ask[int]]) int {
			return reader(r, 2, func(b Cap[Ask[int]]) int {
				return Perform(a, Ask[int]{})*10 + Perform(b, Ask[int]{})
			})
		})
	})
	if got != 12 {
		t.Fatalf("told apart by capability: want 12, got %d", got)
	}
}

func TestDifferentTypeArguments(t *testing.T) {
	got := Main(func(r *Run) string {
		return reader(r, 3, func(n Cap[Ask[int]]) string {
			return reader(r, "ab", func(s Cap[Ask[string]]) string {
				return strings.Repeat(Perform(s, Ask[string]{}), Perform(n, Ask[int]{}))
			})
		})
	})
	if got != "ababab" {
		t.Fatalf("want ababab, got %q", got)
	}
}

func TestEscapedCapabilityRefused(t *testing.T) {
	var kept Cap[Ask[int]]
	Main(func(r *Run) int { return reader(r, 1, func(a Cap[Ask[int]]) int { kept = a; return 0 }) })
	defer func() {
		p := recover()
		if p == nil || !strings.Contains(p.(string), "escaped its scope") {
			t.Fatalf("want an escape refused by name, got %v", p)
		}
	}()
	Perform(kept, Ask[int]{})
}

func TestEscapedInsideAnotherHandler(t *testing.T) {
	var kept Cap[Ask[int]]
	defer func() {
		p := recover()
		if p == nil || !strings.Contains(p.(string), "escaped its scope") {
			t.Fatalf("want an escape refused by name, got %v", p)
		}
	}()
	Main(func(r *Run) int {
		reader(r, 1, func(a Cap[Ask[int]]) int { kept = a; return 0 })
		return reader(r, 2, func(Cap[Ask[int]]) int { return Perform(kept, Ask[int]{}) })
	})
}

func TestStreamBreakRunsDefers(t *testing.T) {
	var got []int
	cleaned := false
	Main(func(r *Run) int {
		return reader(r, 5, func(step Cap[Ask[int]]) int {
			for v := range Stream(r, func(out Cap[Emit[int]]) {
				defer func() { cleaned = true }()
				for i := 0; ; i += Perform(step, Ask[int]{}) {
					Yield(out, i)
				}
			}) {
				if v > 20 {
					break
				}
				got = append(got, v)
			}
			return 0
		})
	})
	if len(got) != 5 || got[4] != 20 || !cleaned {
		t.Fatalf("want 0..20 step 5 and the producer's defer run, got %v cleaned=%v", got, cleaned)
	}
}

func TestMillionOperations(t *testing.T) {
	got := Main(func(r *Run) int {
		return reader(r, 1, func(a Cap[Ask[int]]) int {
			s := 0
			for i := 0; i < 1_000_000; i++ {
				s += Perform(a, Ask[int]{})
			}
			return s
		})
	})
	if got != 1_000_000 {
		t.Fatal(got)
	}
}

func BenchmarkPerform(b *testing.B) {
	Main(func(r *Run) int {
		return reader(r, 1, func(a Cap[Ask[int]]) int {
			s := 0
			for i := 0; i < b.N; i++ {
				s += Perform(a, Ask[int]{})
			}
			return s
		})
	})
}

func BenchmarkPerformThroughAnotherHandler(b *testing.B) {
	Main(func(r *Run) int {
		return reader(r, 1, func(a Cap[Ask[int]]) int {
			return reader(r, "x", func(Cap[Ask[string]]) int {
				s := 0
				for i := 0; i < b.N; i++ {
					s += Perform(a, Ask[int]{})
				}
				return s
			})
		})
	})
}
