package shop

import (
	"strings"
	"testing"
)

func TestTestAssembly(t *testing.T) {
	var sent []string
	app := Test(map[string]int{"book": 1}, false, &sent)
	got := app(func(s Shop) Receipt { return s.Checkout("ann", "book", 2000) })
	if got != (Receipt{false, "declined"}) || sent[0] != "ann: not paid: declined" {
		t.Fatalf("%v %v", got, sent)
	}
}

func TestProductionAssembly(t *testing.T) {
	var calls, sent []string
	app := Production(map[string]int{"book": 1}, &calls, &sent)
	got := app(func(s Shop) Receipt { return s.Checkout("bob", "book", 2000) })
	if got != (Receipt{true, "ch_42"}) || len(calls) != 1 ||
		calls[0] != "POST https://api.stripe.com/v1/charges customer=bob&amount=2000" ||
		sent[0] != "bob: paid, receipt ch_42" {
		t.Fatalf("%v %v %v", got, calls, sent)
	}
}

func TestOutOfStockNeverCharges(t *testing.T) {
	var calls, sent []string
	app := Production(map[string]int{"book": 0}, &calls, &sent)
	got := app(func(s Shop) Receipt { return s.Checkout("eve", "book", 2000) })
	if got.Paid || !strings.Contains(got.Detail, "out of stock") || len(calls) != 0 {
		t.Fatalf("%v %v", got, calls)
	}
}
