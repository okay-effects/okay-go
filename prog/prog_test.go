package prog

import (
	"reflect"
	"testing"
)

type Flip struct{ Answers[bool] }

// every branch: k resumed twice
func all[A any](body func(Cap[Flip]) Prog[A]) Prog[[]A] {
	return Control(func(a A) Prog[[]A] { return Pure([]A{a}) },
		func(_ Flip, k func(bool) Prog[[]A]) Prog[[]A] {
			return Then(k(true), func(xs []A) Prog[[]A] {
				return Map(k(false), func(ys []A) []A { return append(append([]A{}, xs...), ys...) })
			})
		}, body)
}

func TestMultiShot(t *testing.T) {
	got := Run(all(func(f Cap[Flip]) Prog[int] {
		return Then(Perform(f, Flip{}), func(a bool) Prog[int] {
			return Map(Perform(f, Flip{}), func(b bool) int {
				r := 0
				if a {
					r += 1
				}
				if b {
					r += 2
				}
				return r
			})
		})
	}))
	if !reflect.DeepEqual(got, []int{3, 1, 2, 0}) {
		t.Fatal(got)
	}
}

// backtracking: choose among values, fail a branch, collect what survives
type Choose struct {
	Answers[int]
	From, To int
}
type Fail struct{ Answers[struct{}] }

func TestPythagoreanTriples(t *testing.T) {
	type T = [3]int
	// Choose outside, collecting every branch's answers; Fail inside, ending only its branch
	collect := func(body func(Cap[Choose], Cap[Fail]) Prog[T]) []T {
		return Run(Control(Pure[[]T],
			func(c Choose, k func(int) Prog[[]T]) Prog[[]T] {
				acc := Pure[[]T](nil)
				for v := c.To; v >= c.From; v-- {
					v, rest := v, acc
					acc = Then(k(v), func(xs []T) Prog[[]T] {
						return Map(rest, func(ys []T) []T { return append(append([]T{}, xs...), ys...) })
					})
				}
				return acc
			},
			func(ch Cap[Choose]) Prog[[]T] {
				return Control(func(a T) Prog[[]T] { return Pure([]T{a}) },
					func(_ Fail, _ func(struct{}) Prog[[]T]) Prog[[]T] { return Pure[[]T](nil) },
					func(fail Cap[Fail]) Prog[T] { return body(ch, fail) })
			}))
	}
	got := collect(func(ch Cap[Choose], fail Cap[Fail]) Prog[T] {
		return Then(Perform(ch, Choose{From: 1, To: 20}), func(a int) Prog[T] {
			return Then(Perform(ch, Choose{From: a, To: 20}), func(b int) Prog[T] {
				return Then(Perform(ch, Choose{From: b, To: 20}), func(c int) Prog[T] {
					if a*a+b*b != c*c {
						return Then(Perform(fail, Fail{}), func(struct{}) Prog[T] { return Pure(T{}) })
					}
					return Pure(T{a, b, c})
				})
			})
		})
	})
	want := []T{{3, 4, 5}, {5, 12, 13}, {6, 8, 10}, {8, 15, 17}, {9, 12, 15}, {12, 16, 20}}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

type Ask[T any] struct{ Answers[T] }

func TestTwoInstancesAndInto(t *testing.T) {
	// two readers of one type, told apart by capability; a third translated into them
	type Total struct{ Answers[int] }
	got := Run(Answer(func(Ask[int]) int { return 1 }, func(a Cap[Ask[int]]) Prog[int] {
		return Answer(func(Ask[int]) int { return 2 }, func(b Cap[Ask[int]]) Prog[int] {
			return Into(func(Total) Prog[int] {
				return Then(Perform(a, Ask[int]{}), func(x int) Prog[int] {
					return Map(Perform(b, Ask[int]{}), func(y int) int { return x*10 + y })
				})
			}, func(tc Cap[Total]) Prog[int] { return Perform(tc, Total{}) })
		})
	}))
	if got != 12 {
		t.Fatal(got)
	}
}

// a pause: the handler hands k out as a value; the program is resumed later, from outside
type Question struct {
	Answers[int]
	Text string
}
type Paused struct {
	Asked  string
	Resume func(int) Paused
	Done   bool
	Value  int
}

func TestPauseAndResumeLater(t *testing.T) {
	sum := func(q Cap[Question]) Prog[int] {
		return Then(Perform(q, Question{Text: "a?"}), func(a int) Prog[int] {
			return Map(Perform(q, Question{Text: "b?"}), func(b int) int { return a + b })
		})
	}
	p := Run(Control(func(v int) Prog[Paused] { return Pure(Paused{Done: true, Value: v}) },
		func(op Question, k func(int) Prog[Paused]) Prog[Paused] {
			return Pure(Paused{Asked: op.Text, Resume: func(n int) Paused { return Run(k(n)) }})
		}, sum))
	if p.Asked != "a?" {
		t.Fatal(p)
	}
	p2 := p.Resume(2)
	if p2.Asked != "b?" {
		t.Fatal(p2)
	}
	if done := p2.Resume(3); !done.Done || done.Value != 5 {
		t.Fatal(done)
	}
	// and resumed again from the first pause, another way: multi-shot
	if other := p.Resume(10).Resume(20); other.Value != 30 {
		t.Fatal(other)
	}
}

func TestMillionDeepInConstantStack(t *testing.T) {
	var even, odd func(n int) Prog[bool]
	even = func(n int) Prog[bool] {
		if n == 0 {
			return Pure(true)
		}
		return Defer(func() Prog[bool] { return odd(n - 1) })
	}
	odd = func(n int) Prog[bool] {
		if n == 0 {
			return Pure(false)
		}
		return Defer(func() Prog[bool] { return even(n - 1) })
	}
	if !Run(even(1_000_000)) {
		t.Fatal("even")
	}
	got := Run(Answer(func(Ask[int]) int { return 1 }, func(a Cap[Ask[int]]) Prog[int] {
		var loop func(n, acc int) Prog[int]
		loop = func(n, acc int) Prog[int] {
			if n == 0 {
				return Pure(acc)
			}
			return Then(Perform(a, Ask[int]{}), func(x int) Prog[int] { return loop(n-1, acc+x) })
		}
		return loop(1_000_000, 0)
	}))
	if got != 1_000_000 {
		t.Fatal(got)
	}
}

func TestUnhandledRefused(t *testing.T) {
	var kept Cap[Ask[int]]
	Run(Answer(func(Ask[int]) int { return 1 }, func(a Cap[Ask[int]]) Prog[int] { kept = a; return Pure(0) }))
	defer func() {
		if recover() == nil {
			t.Fatal("an escaped capability must be refused")
		}
	}()
	Run(Perform(kept, Ask[int]{}))
}
