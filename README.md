# okay-go

Algebraic effects for Go, in direct style, with capabilities: the design of
[okay](https://github.com/sergey-scherbina/okay)'s okay-java `Cap`, on Go's own coroutines.

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
Continuations are one-shot (a coroutine cannot be copied); multi-shot needs
programs as data, okay's `okay.Prog` (docs/go.md in okay).

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

app := shop.Production(items, &calls, &sent)                      // the system, assembled once
receipt := app(func(s Shop) Receipt { return s.Checkout("bob", "book", 2000) })
```

`examples/shop`: the services, their handlers (`Stock`, `Card`, `Gateway`,
`Stripe` — payments over HTTP — `Mailbox`), a test and a production assembly.

## Status

An experiment: the core (`okay.go`) and its tests. One `Run` belongs to one
goroutine; parallel branches and `Async` over channels are the next step.
