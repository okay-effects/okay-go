package shop

import (
	"testing"

	okay "github.com/sergey-scherbina/okay-go"
)

// what an interface cannot do, 1: the handler ends the whole Checkout, not the call
func Limit[A any](r *okay.Run, limit int64, pay Payments, stop func(Charge) A, body func(Payments) A) A {
	return okay.Handle(r, func(op Charge) any {
		if op.Cents > limit {
			return okay.Abort(stop(op)) // Checkout stops here: no mail, its defers run
		}
		return pay.Charge(op.User, op.Cents)
	}, func(c okay.Cap[Charge]) A { return body(Payments{c}) })
}

func TestALimitEndsTheCheckout(t *testing.T) {
	var sent []string
	got := okay.Main(func(r *okay.Run) Receipt {
		return Mailbox(r, &sent, func(mail Notify) Receipt {
			return Card(r, true, func(card Payments) Receipt {
				return Limit(r, 1000, card, func(op Charge) Receipt { return Receipt{false, "needs approval"} },
					func(pay Payments) Receipt {
						return Table(r, map[string]int{"book": 1}, func(db Db) Receipt {
							return Stock(r, db, func(inv Inventory) Receipt {
								return Shop{inv, pay, mail}.Checkout("bob", "book", 2000)
							})
						})
					})
			})
		})
	})
	if got != (Receipt{false, "needs approval"}) || len(sent) != 0 {
		t.Fatal(got, sent)
	}
}

// what an interface cannot do, 2: the checkout stopped at Charge is a value — answered later, from outside
func TestACheckoutWaitsForAManager(t *testing.T) {
	run := okay.Start(func(r *okay.Run, h okay.Host) Receipt {
		return Remote(h).Checkout("bob", "book", 2000)
	})
	s := run.Step()
	if s.Op != (Reserve{Item: "book"}) {
		t.Fatal(s)
	}
	s = run.Resume(true)
	if s.Op != (Charge{User: "bob", Cents: 2000}) {
		t.Fatal(s)
	}
	pending := run // kept as a value: its caller is not stuck inside a call, it holds the operation as data
	s = pending.Resume(Receipt{true, "approved by ann"})
	if s.Op != (Email{To: "bob", Text: "paid, receipt approved by ann"}) {
		t.Fatal(s)
	}
	if s = pending.Resume(struct{}{}); s.Kind != okay.Done || s.Value.Detail != "approved by ann" {
		t.Fatal(s)
	}
}
