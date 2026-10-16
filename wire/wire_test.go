package wire

import (
	"errors"
	"testing"

	okay "github.com/sergey-scherbina/okay-go"
	"github.com/sergey-scherbina/okay-go/examples/shop"
)

// the other side as a table of named functions, answering plain values — as a foreign host does
type far map[string]func([]any) (any, error)

func (f far) call(name string, args []any) (any, error) {
	if g, ok := f[name]; ok {
		return g(args)
	}
	return nil, errors.New("no " + name)
}

func checkout(user, item string, cents int64) *okay.Computation[shop.Receipt] {
	return okay.Start(func(r *okay.Run, h okay.Host) shop.Receipt {
		return shop.Remote(h).Checkout(user, item, cents)
	})
}

func TestTheShopStepsOverTheWire(t *testing.T) {
	var log []string
	host := far{
		"reserve": func(a []any) (any, error) { return a[0].(map[string]any)["item"] == "book", nil },
		// a receipt as a map, as it would come off the wire
		"charge": func(a []any) (any, error) {
			return map[string]any{"paid": true, "detail": "ch_" + a[0].(map[string]any)["user"].(string)}, nil
		},
		"email": func(a []any) (any, error) {
			m := a[0].(map[string]any)
			log = append(log, m["to"].(string)+": "+m["text"].(string))
			return nil, nil
		},
	}
	r, err := Drive(checkout("bob", "book", 2000), host.call)
	if err != nil || !r.Paid || r.Detail != "ch_bob" || log[0] != "bob: paid, receipt ch_bob" {
		t.Fatal(r, err, log)
	}
	if r, _ := Drive(checkout("eve", "pen", 100), host.call); r.Paid || log[1] != "eve: not paid: pen: out of stock" {
		t.Fatal(r, log)
	}
}

func TestAFailureOverTheWireIsAFailureOfTheProgram(t *testing.T) {
	_, err := Drive(checkout("bob", "book", 2000), far{}.call)
	if err == nil || err.Error() != "okay-go: no reserve" {
		t.Fatal(err)
	}
}

func TestServeAGoHandler(t *testing.T) {
	name, f := Serve(func(op shop.Charge) shop.Receipt { return shop.Receipt{Paid: op.Cents < 5000, Detail: op.User} })
	got, err := f([]any{map[string]any{"user": "ann", "cents": int64(300)}})
	if name != "charge" || err != nil || got.(map[string]any)["paid"] != true {
		t.Fatal(name, got, err)
	}
	// and back: what Serve answers, Drive decodes
	host := far{name: f,
		"reserve": func([]any) (any, error) { return true, nil },
		"email":   func([]any) (any, error) { return nil, nil }}
	if r, err := Drive(checkout("ann", "book", 9000), host.call); err != nil || r.Paid || r.Detail != "ann" {
		t.Fatal(r, err)
	}
}
