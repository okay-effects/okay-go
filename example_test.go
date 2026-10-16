package okay_test

import (
	"fmt"
	"time"

	okay "github.com/sergey-scherbina/okay-go"
)

// An operation: a struct embedding Answers[R], R being what it answers.
type Ask struct{ okay.Answers[int] }

// A handler makes a capability and hands it to its body; the body performs
// through it. Two instances of one effect are told apart by capability.
func Example() {
	got := okay.Main(func(r *okay.Run) int {
		return okay.Answer(r, func(Ask) int { return 1 }, func(outer okay.Cap[Ask]) int {
			return okay.Answer(r, func(Ask) int { return 2 }, func(inner okay.Cap[Ask]) int {
				return okay.Perform(outer, Ask{})*10 + okay.Perform(inner, Ask{})
			})
		})
	})
	fmt.Println(got)
	// Output: 12
}

// A producer's Yields as a Go iterator; break stops the producer, its defers run.
func ExampleStream() {
	okay.Main(func(r *okay.Run) int {
		for n := range okay.Stream(r, func(out okay.Cap[okay.Emit[int]]) {
			defer fmt.Println("producer stopped")
			for i := 1; ; i++ {
				okay.Yield(out, i*i)
			}
		}) {
			if n > 10 {
				break
			}
			fmt.Println(n)
		}
		return 0
	})
	// Output:
	// 1
	// 4
	// 9
	// producer stopped
}

// Branches on goroutines; a parent's capability lent to each, answered by the
// parent's handler one operation at a time.
func ExampleAll() {
	okay.Main(func(r *okay.Run) int {
		total := 0
		okay.Answer(r, func(op Ask) int { total++; return total }, func(c okay.Cap[Ask]) int {
			okay.All(r, func(br *okay.Run) int {
				okay.Sleep(br, 10*time.Millisecond)
				return okay.Perform(okay.Lend(br, c), Ask{})
			}, func(br *okay.Run) int {
				return okay.Perform(okay.Lend(br, c), Ask{})
			})
			return 0
		})
		fmt.Println("asked", total, "times")
		return 0
	})
	// Output: asked 2 times
}

// A program handed out and stepped by a host that sees only operations and answers.
func ExampleStart() {
	run := okay.Start(func(r *okay.Run, h okay.Host) int {
		ask := okay.Outside[Ask](h)
		return okay.Perform(ask, Ask{}) + okay.Perform(ask, Ask{})
	})
	s := run.Step()
	for s.Kind == okay.Yielded {
		fmt.Printf("%T\n", s.Op)
		s = run.Resume(21)
	}
	fmt.Println(s.Value)
	// Output:
	// okay_test.Ask
	// okay_test.Ask
	// 42
}
