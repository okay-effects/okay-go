package shop

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	okay "github.com/sergey-scherbina/okay-go"
)

// the shop's services answered in one place, every operation passing the same middleware
func services(stock map[string]int, sent *[]string) okay.Routes {
	rs := okay.Routes{}
	okay.Route(rs, func(op Reserve) (bool, error) {
		if stock[op.Item] == 0 {
			return false, nil
		}
		stock[op.Item]--
		return true, nil
	})
	okay.Route(rs, func(op Charge) (Receipt, error) { return Receipt{true, "ch_" + op.User}, nil })
	okay.Route(rs, func(op Email) (struct{}, error) { *sent = append(*sent, op.To+": "+op.Text); return struct{}{}, nil })
	return rs
}

func checkout(user string, cents int64) *okay.Computation[Receipt] {
	return okay.Start(func(r *okay.Run, h okay.Host) Receipt { return Remote(h).Checkout(user, "book", cents) })
}

func TestEveryOperationThroughTheSameMiddleware(t *testing.T) {
	var sent, audit []string
	counts := map[string]int{}
	var recorded []okay.Entry

	// all of them alike: an audit trail, a metric, a recording
	auditing := func(op any, next okay.Answerer) (any, error) {
		v, err := next(op)
		audit = append(audit, fmt.Sprintf("%T %+v -> %v", op, op, v))
		return v, err
	}
	metrics := func(op any, next okay.Answerer) (any, error) {
		counts[reflect.TypeOf(op).Name()]++
		return next(op)
	}
	// one of them its own way: only these users may be charged, above 5000 nobody
	allowed := map[string]bool{"bob": true}
	security := okay.Only(func(op Charge, next okay.Answerer) (any, error) {
		if !allowed[op.User] || op.Cents > 5000 {
			return nil, errors.New("forbidden: charge " + op.User)
		}
		return next(op)
	})

	answer := okay.With(services(map[string]int{"book": 5}, &sent).Answer,
		auditing, metrics, okay.Record(&recorded), security)

	r, err := okay.Drive(checkout("bob", 2000), answer)
	if err != nil || r != (Receipt{true, "ch_bob"}) {
		t.Fatal(r, err)
	}
	if len(audit) != 3 || audit[1] != "shop.Charge {Answers:{} User:bob Cents:2000} -> {true ch_bob}" {
		t.Fatal(audit)
	}
	// refused by security: raised where Charge was performed, so no mail goes out
	if _, err := okay.Drive(checkout("eve", 100), answer); err == nil || err.Error() != "okay-go: forbidden: charge eve" {
		t.Fatal(err)
	}
	if len(sent) != 1 || counts["Charge"] != 2 || counts["Email"] != 1 {
		t.Fatal(sent, counts)
	}

	// replay: bob's run again from the recording — the same receipt, no service reached
	bob := recorded[:3]
	if r, err := okay.Drive(checkout("bob", 2000), okay.Replay(bob)); err != nil || r != (Receipt{true, "ch_bob"}) {
		t.Fatal(r, err)
	}
	// and a run that does something else is caught at the first difference
	if _, err := okay.Drive(checkout("bob", 3000), okay.Replay(bob)); err == nil {
		t.Fatal("a diverging replay must be refused")
	}
}
