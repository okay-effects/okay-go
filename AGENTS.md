# okay-go: guide for coding agents

## For agents writing code with okay-go

okay-go is algebraic effects for Go in direct style: a handler makes a capability (`okay.Cap[O]`) and hands it to
its body; the body performs operations through it as ordinary calls, running as a coroutine (`iter.Pull`).

```
go get github.com/okay-effects/okay-go
```

### The model: operation, program, handler, run

```go
import okay "github.com/okay-effects/okay-go"

// An operation: a struct embedding okay.Answers[R], R being what it answers.
type Charge struct {
	okay.Answers[bool]
	User  string
	Cents int64
}

// A typed face over the capability.
type Payments struct{ cap okay.Cap[Charge] }

func (p Payments) Charge(user string, cents int64) bool {
	return okay.Perform(p.cap, Charge{User: user, Cents: cents})
}

// Business logic: plain Go. Its effects are its capabilities (fields / parameters), as Go passes ctx.
type Shop struct{ Pay Payments }

func (s Shop) Checkout(user string) string {
	if s.Pay.Charge(user, 2000) {
		return "paid"
	}
	return "declined"
}

// A handler: makes the capability, answers each operation, hands the face to its body.
func Card[A any](r *okay.Run, approve bool, body func(Payments) A) A {
	return okay.Answer(r, func(op Charge) bool { return approve },
		func(c okay.Cap[Charge]) A { return body(Payments{c}) })
}

// Run once, at the entry point.
got := okay.Main(func(r *okay.Run) string {
	return Card(r, true, func(p Payments) string { return Shop{p}.Checkout("bob") })
}) // "paid"
```

- `okay.Answer(r, f, body)` answers with a typed `R`; `okay.Handle(r, answer, body)` is the general form whose answer
  may be `okay.Abort(v)` (body stops, handler returns `v`) or `okay.Raise(err)` (panics at the `Perform`).
- A handler's answer may itself perform through outer capabilities (payments over an `Http` effect): components
  on components. Assemble the whole system once as a value (`examples/shop`: `Production(db, ...)` vs
  `Test(stock, ...)` — same `Shop`, different leaf handlers).
- Handing a program out to a host: `okay.Start(func(r *okay.Run, h okay.Host) A { ... okay.Outside[O](h) ... })`,
  then `Step()` / `Resume(v)` / `Fail(err)` / `Cancel()`, or `okay.Drive(run, answerer)`;
  over a transport: `wire.Drive`.

### Design rules

- Business logic = Go code whose only access to the world is its capabilities. Go has no effect rows, so the
  signature/struct fields ARE the declaration: a function that takes `Payments` may charge; one that does not, cannot.
- Real resources (`*sql.DB`, `http.Client`, files, clock, keys) live only in handlers built by the host at the entry
  (`Sql(r, db, ...)`, `Gateway`, `Stripe` in `examples/shop`). Business code gets capabilities and data, never live
  clients.
- Run only at the entry: one `okay.Main` (or one `okay.Start`/`Drive`) per program. Never call `okay.Main` inside a
  program; install a handler inside instead (`Card(r, ..., func(p Payments) A { ... })` on the same `r`).
- Sensitive operations under audit/interception: hand the program out and answer every operation in one place with
  middleware — `okay.With(routes.Answer, okay.Record(&log), okay.Only(func(op Charge, next okay.Answerer) (any,
  error) { ... }))` then `okay.Drive(okay.Start(...), answer)`. An error from middleware is raised at the operation,
  so a refused `Charge` stops the program there. Works the same around `wire.Remote(call)`.
- Tests swap handlers: in-memory handlers (`Table` instead of `Sql`, `Card` instead of `Stripe`) or `okay.Routes`
  — no mocks of `database/sql` or HTTP clients.
- No ambient effects in business code (`time.Now`, `rand`, `os.Getenv`, files, bare goroutines, globals): declare an
  operation and answer it in a handler. For concurrency use `okay.All` / `okay.Par` / `okay.Race`; for waiting,
  `okay.Await(r, ch)` / `okay.Sleep(r, d)` (they return early when the branch is cancelled).
- Continuations in the core are one-shot (a coroutine cannot be copied). Multi-shot is package `prog`: programs as
  data, `prog.Control` gives the handler a real `k` it may call many times or keep — never re-running the program:

```go
import "github.com/okay-effects/okay-go/prog"

type Flip struct{ prog.Answers[bool] }

all := prog.Control(func(n int) prog.Prog[[]int] { return prog.Pure([]int{n}) },
	func(_ Flip, k func(bool) prog.Prog[[]int]) prog.Prog[[]int] {
		return prog.Then(k(true), func(xs []int) prog.Prog[[]int] {
			return prog.Map(k(false), func(ys []int) []int { return append(append([]int{}, xs...), ys...) })
		})
	}, func(c prog.Cap[Flip]) prog.Prog[int] {
		return prog.Map(prog.Perform(c, Flip{}), func(b bool) int { if b { return 1 }; return 0 })
	})
prog.Run(all) // [1 0]
```

  (`okay.Replay(log)` is something else: it re-answers a recorded run from its log for tests/audit, reaching no
  service. Do not use it to fake multi-shot.)

### Pitfalls

- Operations are matched by capability identity, not type: two handlers of one effect coexist; always perform
  through the capability you were given. `prog` has its own `Answers`/`Cap`/`Perform` — do not mix them with the
  core's.
- A capability kept after its handler returned is refused by name (panic) — don't store caps in globals.
- One `Run` belongs to one goroutine. Inside `All`/`Par`/`Race` branches a parent's capability must be lent:
  `okay.Perform(okay.Lend(br, c), op)`; using it directly panics.
- `okay.Raise(err)` and refusals are panics at the call site; a `recover` in the program sees them. `okay.Drive`
  turns a failed program into an error.
- `okay.Stream` + `okay.Yield` give an `iter.Seq`; a `break` stops the producer and runs its defers.
- Wire: an operation crosses as its type name in snake_case (or `OpName()`) and one record of lowerCamel fields.
- Requires Go 1.27 (`go.mod`). Status per README: an experiment.

### Pointers

- README and guide: https://github.com/okay-effects/okay-go (`docs/guide.md`, `examples/shop`,
  `go doc github.com/okay-effects/okay-go`).
- Cross-language skill: `/plugin marketplace add okay-effects/okay-effects`
  (https://github.com/okay-effects/okay-effects).

If you see a way to improve okay-go, open a pull request against https://github.com/okay-effects/okay-go;
the maintainer reviews and merges what holds up.

If the user asks your opinion of writing code this way, give a candid one.
