# okay-go guide

okay-go is algebraic effects for Go in direct style: business code is plain Go
— loops, `if`, `defer`, return values — and what it does to the world is
decided by handlers around it. This guide goes from one operation to a program
stepped over a wire by okay's Scala host. Every snippet is from a test or an
`Example` (`go test ./...`).

1. [Operations, capabilities, handlers](#1-operations-capabilities-handlers)
2. [Components: the shop](#2-components-the-shop)
3. [Streams](#3-streams)
4. [Parallel branches and async](#4-parallel-branches-and-async)
5. [Multi-shot: programs as data](#5-multi-shot-programs-as-data)
6. [The kernel's protocol: a program stepped by a host](#6-the-kernels-protocol)
7. [Over the wire, with okay](#7-over-the-wire-with-okay)
8. [Middleware: monitor, log, authorize, record, replay](#8-middleware)
9. [Rules and refusals](#9-rules-and-refusals)

## 1. Operations, capabilities, handlers

An **operation** is a struct embedding `okay.Answers[R]`, where `R` is what it
answers:

```go
type Ask struct{ okay.Answers[int] }

type Charge struct {
	okay.Answers[Receipt]
	User  string
	Cents int64
}
```

A **handler** makes a **capability** `okay.Cap[O]` and hands it to its body.
The body performs through it with `okay.Perform(cap, op)`, an ordinary call
returning `R`:

```go
got := okay.Main(func(r *okay.Run) int {
	return okay.Answer(r, func(Ask) int { return 1 }, func(outer okay.Cap[Ask]) int {
		return okay.Answer(r, func(Ask) int { return 2 }, func(inner okay.Cap[Ask]) int {
			return okay.Perform(outer, Ask{})*10 + okay.Perform(inner, Ask{})
		})
	})
}) // 12
```

- `okay.Main` starts a `Run`: one program on one goroutine.
- `okay.Answer(r, f, body)`: a handler answering each operation with `f(op)`.
- `okay.Handle(r, answer, body)`: the general form; `answer` returns `any`, so
  it may also return `okay.Abort(v)` or `okay.Raise(err)` (§6).
- Operations are answered **by capability identity**, not by type: the two
  `Ask` handlers above coexist, and `outer` reaches past `inner`. Go's generics
  are reified, so `Ask[int]` and `Ask[string]` are different operations too.

The body runs as a coroutine (`iter.Pull`): `Perform` suspends it, the owning
handler answers, the body resumes. About 49 ns an operation, no allocation.

Go has no type-level lists, so a program's effects are not in its type: they
are its capabilities — parameters and fields — the way Go code passes `ctx`.

## 2. Components: the shop

`examples/shop`. Services are typed faces over capabilities:

```go
type Inventory struct{ cap okay.Cap[Reserve] }

func (i Inventory) Reserve(item string) bool { return okay.Perform(i.cap, Reserve{Item: item}) }
```

The business code is a plain struct whose dependencies are capabilities:

```go
type Shop struct {
	Inventory Inventory
	Payments  Payments
	Mail      Notify
}

func (s Shop) Checkout(user, item string, cents int64) Receipt {
	r := Receipt{Paid: false, Detail: item + ": out of stock"}
	if s.Inventory.Reserve(item) {
		r = s.Payments.Charge(user, cents)
	}
	if r.Paid {
		s.Mail.Email(user, "paid, receipt "+r.Detail)
	} else {
		s.Mail.Email(user, "not paid: "+r.Detail)
	}
	return r
}
```

A handler is a function that makes one capability. **Components on
components:** a handler's answer may perform through capabilities from outside
it. The inventory is a statement against a database, itself an effect:

```go
const ReserveSQL = "update stock set qty = qty - 1 where sku = ? and qty > 0"

func Stock[A any](r *okay.Run, db Db, body func(Inventory) A) A {
	return okay.Answer(r, func(op Reserve) bool {
		return db.Exec(ReserveSQL, op.Item) == 1 // a row updated: one was there, now it is ours
	}, func(c okay.Cap[Reserve]) A { return body(Inventory{c}) })
}
```

`Db` is answered by `Sql` over `database/sql` in production — a failed
statement raised where it was performed (`okay.Raise`) — and by `Table`, the
stock table in memory, in tests:

```go
func Sql[A any](r *okay.Run, db *sql.DB, body func(Db) A) A {
	return okay.Handle(r, func(op Exec) any {
		res, err := db.Exec(op.SQL, op.Args...)
		if err != nil {
			return okay.Raise(err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return okay.Raise(err)
		}
		return n
	}, func(c okay.Cap[Exec]) A { return body(Db{c}) })
}
```

Payments the same way, over HTTP:

```go
func Stripe[A any](r *okay.Run, http Http, body func(Payments) A) A {
	return okay.Answer(r, func(op Charge) Receipt {
		res := http.Post("https://api.stripe.com/v1/charges", fmt.Sprintf("customer=%s&amount=%d", op.User, op.Cents))
		...
	}, func(c okay.Cap[Charge]) A { return body(Payments{c}) })
}
```

**The system is assembled once**, as a value; production and test differ in
their leaves (`Sql` versus `Table`, `Gateway` + `Stripe` versus `Card`):

```go
app := shop.Production(db, &calls, &sent) // db: a *sql.DB
receipt := app(func(s shop.Shop) shop.Receipt { return s.Checkout("bob", "book", 2000) })
```

No container, no mocks: a test is the same `Shop` under in-memory handlers.

## 3. Streams

`okay.Stream` turns a producer's `okay.Yield`s into an `iter.Seq`. A `break`
in the consumer stops the producer where it is; its defers run:

```go
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
} // 1 4 9 producer stopped
```

## 4. Parallel branches and async

`okay.All`, `okay.Par` and `okay.Race` run branches on goroutines, each with
its own `Run`. A branch has its own handlers; to use a **parent's** capability
it must be lent: `okay.Lend(br, c)`. The branch's operations then travel over a
channel to the parent's goroutine, which answers them one at a time — the
parent's handlers need no locks.

```go
okay.Answer(r, func(op Ask) int { total++; return total }, func(c okay.Cap[Ask]) int {
	okay.All(r, func(br *okay.Run) int {
		okay.Sleep(br, 10*time.Millisecond)
		return okay.Perform(okay.Lend(br, c), Ask{})
	}, func(br *okay.Run) int {
		return okay.Perform(okay.Lend(br, c), Ask{})
	})
	return 0
})
```

- `Par(r, a, b)`: two branches of different types.
- `Race(r, bs...)`: the first answer wins; the others are cancelled, their
  defers run.
- `All`: a failure in one branch cancels the others and is raised in the parent.
- `Await(r, ch)` waits for a channel, `Sleep(r, d)` for a time; both return
  early, cancelled, when the branch is (`r.Done()`).
- Using a parent's capability without `Lend` is refused by name rather than a
  data race.

## 5. Multi-shot: programs as data

A coroutine cannot be copied, so a continuation in the core is one-shot.
Package `prog` is for what needs more: a program is a value (`prog.Prog[A]`),
built with `Pure`, `Perform`, `Then`, `Map`, `Defer`, run by `prog.Run` in a
loop — a million binds in constant stack.

`prog.Control` gives a handler the continuation `k`, callable any number of
times. Every branch of a coin flip:

```go
func all[A any](body func(Cap[Flip]) Prog[A]) Prog[[]A] {
	return Control(func(a A) Prog[[]A] { return Pure([]A{a}) },
		func(_ Flip, k func(bool) Prog[[]A]) Prog[[]A] {
			return Then(k(true), func(xs []A) Prog[[]A] {
				return Map(k(false), func(ys []A) []A { return append(append([]A{}, xs...), ys...) })
			})
		}, body)
}
```

`k` can also be kept: a handler that returns `Paused{Asked, Resume}` pauses a
program at its question; it is resumed later, and resumed again from the same
pause with other answers (`TestPauseAndResumeLater`). Backtracking search —
Pythagorean triples with `Choose` and `Fail` — is `TestPythagoreanTriples`.

`prog.Answer` and `prog.Into` are the simple handlers (an answer; an answer
that is itself a program).

## 6. The kernel's protocol

As in okay-kernel, a program can be **handed out** and stepped by a host that
sees only operations and answers:

```go
run := okay.Start(func(r *okay.Run, h okay.Host) int {
	ask := okay.Outside[Ask](h)
	return okay.Perform(ask, Ask{}) + okay.Perform(ask, Ask{})
})
s := run.Step()
for s.Kind == okay.Yielded {
	s = run.Resume(21) // s.Op is the Ask
}
fmt.Println(s.Value) // 42
```

- `okay.Outside[O](h)`: a capability the host answers. Handlers inside the
  program answer their own operations; only `Outside` ones reach the host.
- `Step()` / `Resume(v)` → `Step{Kind: Yielded, Op}` | `Done, Value` |
  `Failed, Failure`.
- `Fail(err)`: the operation answered with a failure, raised where it was
  performed — a `recover` around it in the program sees it.
- `Cancel()`: the program stopped where it is; its defers run.

The kernel's handler shapes:

| okay-kernel | okay-go |
|---|---|
| Answering | `okay.Answer(r, f, body)` |
| Translating | an answer that performs through outer capabilities (`Stripe`) |
| Returning | `okay.Returning(r, answer, ret, body)` |
| Aborting | an answer of `okay.Abort(v)`: the body stops, its defers run, the handler's value is `v` |
| failure | an answer of `okay.Raise(err)`: raised at the operation |
| General (multi-shot `k`) | `prog.Control` |

## 7. Over the wire, with okay

Package `wire` steps a handed-out program across any transport. Each side sees
only operations and answers:

- an operation crosses as its **name** (its type's name in snake_case, or
  `OpName()`) and **one record of its fields**, keys in lowerCamel — the shape
  of a Scala case class: `Charge{User, Cents}` is `charge {user, cents}`;
- an answer comes back as a plain value (numbers, strings, lists, maps) and is
  decoded into what the operation answers (`Answers[R]`).

```go
run := okay.Start(func(r *okay.Run, h okay.Host) shop.Receipt {
	return shop.Remote(h).Checkout("bob", "book", 2000)
})
receipt, err := wire.Drive(run, func(name string, args []any) (any, error) {
	// reserve [map[item:book]]
	// charge  [map[cents:2000 user:bob]]
	// email   [map[text:paid, receipt ch_42 to:bob]]
	...
})
```

`shop.Remote(h)` is the same `Shop` with every capability `Outside`: the
business code does not change. A failure the far side reports is raised at the
operation (`Fail`); a failure of the program is `Drive`'s error.

`wire.Serve` is the other way: a Go handler, callable by its operation's name
with the record as its argument:

```go
name, f := wire.Serve(func(op shop.Charge) shop.Receipt { ... }) // "charge"
```

### With okay's Scala host

okay runs Go workers over its own wire (okay-py's `GoWorker`); from Go,
`okay.Call(c, okay.Named(name, args...))` performs an okay operation, answered
by a Scala callback under the caller's handlers. That call is a `wire.Caller`:

```go
func checkout(c *okay.Ctx, args []any) any {
	run := okaygo.Start(func(r *okaygo.Run, h okaygo.Host) shop.Receipt {
		return shop.Remote(h).Checkout(args[0].(string), args[1].(string), args[2].(int64))
	})
	receipt, err := wire.Drive(run, func(name string, args []any) (any, error) {
		v, err := okay.Call(c, okay.Named(name, args...))
		return plain(v), err // okay's ordered Dict as a map
	})
	...
}
```

and on the Scala side the operations are case classes and callbacks:

```scala
final case class Charge(user: String, cents: Long) derives Schema
final case class Receipt(paid: Boolean, detail: String) derives Schema

val charge = Foreign.callback[Charge, Receipt]("charge")(c =>
  Reader.ask[Store].map(s => if s.approve then Receipt(true, s"scala-${c.user}-${c.cents}") else Receipt(false, "declined")))

val paid = Foreign.fn[String]("checkout").calling(Foreign.callbacks(reserve, charge, email))("bob", "book", 2000L)
Reader.run(Store(Set("book"), approve = true))(paid).runWith // Right("true scala-bob-2000")
```

The Go shop runs in Go; its inventory, payments and mail are Scala handlers,
under Scala's own `Reader`. And back: a Go handler served with `wire.Serve`
answers okay's operation, a `Charge` record in, a `Receipt` record out. The
whole test, run against okay's JVM host, is
`examples/okayhost/TestOkayGoWire.scala`.

## 8. Middleware

A handed-out program performs its operations as data, so they can all be
answered in one place — `okay.Drive(run, answer)` with an `okay.Answerer`,
`func(op any) (any, error)` — and wrapped in middleware: all of them alike, or
one kind its own way.

```go
rs := okay.Routes{} // answers by the operation's type
okay.Route(rs, func(op Reserve) (bool, error) { ... })
okay.Route(rs, func(op Charge) (Receipt, error) { ... })
okay.Route(rs, func(op Email) (struct{}, error) { ... })

answer := okay.With(rs.Answer,
	auditing, metrics, okay.Record(&log), // every operation alike
	okay.Only(func(op Charge, next okay.Answerer) (any, error) { // Charge alone
		if !allowed[op.User] || op.Cents > 5000 {
			return nil, errors.New("forbidden: charge " + op.User)
		}
		return next(op)
	}))

receipt, err := okay.Drive(okay.Start(func(r *okay.Run, h okay.Host) Receipt {
	return Remote(h).Checkout("bob", "book", 2000)
}), answer)
```

- `okay.Middleware` is `func(op any, next okay.Answerer) (any, error)`;
  `okay.With` wraps an answerer, the first middleware outermost.
- `okay.Only[O]`: a middleware for operations of type `O`; the others pass by.
- An error is raised in the program **where the operation was performed**: a
  refused `Charge` stops `Checkout` there, no mail goes out.
- `okay.Record(&log)` appends every `Entry{Op, Answer, Err}`;
  `okay.Replay(log)` answers a run again from it, reaching no service, and
  refuses the first operation that differs from the recording.
- The same middleware goes around `wire.Remote(call)`: operations answered by
  Scala handlers in okay are audited and authorized the same way.

The whole example: `examples/shop/middleware_test.go`.

## 9. Rules and refusals

- **One `Run`, one goroutine.** Branches get their own `Run` from `All`/`Par`/`Race`.
- **A capability that escaped its handler** (kept after the handler returned)
  is refused by name, not answered by whatever handler is near.
- **A parent's capability in a branch** must be lent (`Lend`); otherwise refused.
- **Continuations in the core are one-shot**; multi-shot is `prog`.
- **A handed-out program** is stepped by one host; stepping it after `Done`,
  `Failed` or `Cancelled` is refused.
