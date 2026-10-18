# okay-go

Algebraic effects for Go, in direct style, with capabilities: the design of
[okay](https://github.com/okay-effects/okay)'s okay-java `Cap`, on Go's own coroutines.

- **A handler makes a capability and hands it to its body.** The body performs
  operations through it as ordinary calls; ordinary Go code — loops, `if`, `defer`.
- **Programs are coroutines** (`iter.Pull`): performing suspends the program, the
  handler that owns the capability answers, the program resumes. 49 ns an
  operation, no allocation (`go test -bench .`).
- **By identity, not by type.** A handler answers only its own capability's
  operations; two instances of one effect coexist; an operation of another passes
  outward. Go's generics are not erased, so `Ask[int]` and `Ask[string]` differ too.
- **A capability that escaped its handler is refused by name**, not answered by
  whatever handler is near.
- **Components on components.** A handler's answer may perform through capabilities
  from outside it: payments over HTTP.
- **Streams are Go iterators.** `Stream` turns a producer's `Yield`s into an
  `iter.Seq`; a `break` stops the producer where it is, its defers run.

Go has no type-level lists, so a program's effects cannot be in its type: they
are its capabilities — parameters and fields, as a Go program passes `ctx`.
Continuations are one-shot (a coroutine cannot be copied); multi-shot is in
package `prog`, programs as data.

**Documentation:** [the guide](docs/guide.md) and the package docs
(`go doc github.com/okay-effects/okay-go`, runnable `Example`s).

```
go get github.com/okay-effects/okay-go
```

## The shop

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

app := shop.Production(db, &calls, &sent)                         // the system, assembled once
receipt := app(func(s Shop) Receipt { return s.Checkout("bob", "book", 2000) })
```

`examples/shop`: the services, their handlers (`Stock` over a `Db` effect —
`Sql` on `database/sql`, `Table` in memory — `Card`, `Gateway`,
`Stripe` — payments over HTTP — `Mailbox`), a test and a production assembly.

## Parallel branches and async (`par.go`)

`All`, `Par` and `Race` run branches on goroutines. A branch performs through a
capability only once it is lent to it (`Lend`): its operations travel over a
channel to the goroutine that owns the handler, which answers them one at a
time, so handlers need no locks. `Await` waits for a channel, `Sleep` for a
time; a `Race` loser and the others of a failed `All` are cancelled, their
defers run.

## Multi-shot: programs as data (`prog`)

`prog.Prog[A]` is a program as a value: `Pure`, `Perform`, `Then`, `Map`.
`Control` gives a handler the continuation `k`, callable any number of times:
every branch of a choice, a program paused and resumed later, twice. A million
binds run in constant stack.

## The kernel's protocol (`kernel.go`)

As in okay-kernel, a program can be handed out and stepped by a host that sees
only operations and answers:

```go
run := okay.Start(func(r *okay.Run, h okay.Host) Receipt {
	return shop.Remote(h).Checkout("bob", "book", 2000) // capabilities: okay.Outside[Reserve](h), ...
})
s := run.Step()          // Yielded: s.Op is a Reserve
s = run.Resume(true)     // ... Fail(err) raises err at the operation; Cancel() runs its defers
```

The kernel's handler shapes: `Answer` (Answering), an answer performing
through outer capabilities (Translating), `Returning`, an answer of `Abort(v)`
(Aborting), `Raise(err)`, and `prog.Control` (General).

## Over the wire (`wire`)

`wire.Drive` steps a handed-out program through any `Caller` —
`func(name string, args []any) (any, error)`: an operation crosses as its name
and one record of its fields (`charge {user, cents}`), its answer comes back as
a plain value decoded into what the operation answers. `wire.Serve` is the other
way: a Go handler callable by its operation's name. No transport dependency.

With okay's Go worker (`okay.Call(c, okay.Named(name, args...))` as the
Caller), the shop runs in Go while its inventory, payments and mail are Scala
handlers in okay, and a Go handler answers okay's operations:
`examples/okayhost/TestOkayGoWire.scala`, run against okay's JVM host.

## Status

An experiment: the core, parallel branches, programs as data, the kernel's
protocol and the wire, with tests (`go test -race ./...`). One `Run` belongs
to one goroutine; branches borrow capabilities explicitly.

## License

Apache 2.0, as okay.
