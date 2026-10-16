package shop

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	okay "github.com/sergey-scherbina/okay-go"
)

// a database for the production assembly: a database/sql driver keeping the
// stock table in memory, so the test goes through database/sql itself
type stockDb struct {
	qty  map[string]int
	seen []string
}

func (d *stockDb) Connect(context.Context) (driver.Conn, error) { return conn{d}, nil }
func (d *stockDb) Driver() driver.Driver                        { return nil }

type conn struct{ d *stockDb }

func (c conn) Prepare(q string) (driver.Stmt, error) { return stmt{c.d, q}, nil }
func (c conn) Close() error                          { return nil }
func (c conn) Begin() (driver.Tx, error)             { return nil, errors.New("no transactions") }

type stmt struct {
	d *stockDb
	q string
}

func (s stmt) Close() error  { return nil }
func (s stmt) NumInput() int { return -1 }
func (s stmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, errors.New("no queries")
}
func (s stmt) Exec(args []driver.Value) (driver.Result, error) {
	s.d.seen = append(s.d.seen, s.q)
	if s.q != ReserveSQL {
		return nil, errors.New("syntax error at: " + s.q)
	}
	sku := args[0].(string)
	if s.d.qty[sku] == 0 {
		return driver.RowsAffected(0), nil
	}
	s.d.qty[sku]--
	return driver.RowsAffected(1), nil
}

// a database that cannot be reached
type down struct{}

func (down) Connect(context.Context) (driver.Conn, error) { return nil, errors.New("database is down") }
func (down) Driver() driver.Driver                        { return nil }

func brokenDb() *sql.DB { return sql.OpenDB(down{}) }

func open(qty map[string]int) (*sql.DB, *stockDb) {
	d := &stockDb{qty: qty}
	return sql.OpenDB(d), d
}

func TestTestAssembly(t *testing.T) {
	var sent []string
	stock := map[string]int{"book": 1}
	app := Test(stock, false, &sent)
	got := app(func(s Shop) Receipt { return s.Checkout("ann", "book", 2000) })
	if got != (Receipt{false, "declined"}) || sent[0] != "ann: not paid: declined" || stock["book"] != 0 {
		t.Fatalf("%v %v %v", got, sent, stock)
	}
}

func TestProductionAssembly(t *testing.T) {
	var calls, sent []string
	db, d := open(map[string]int{"book": 1})
	app := Production(db, &calls, &sent)
	got := app(func(s Shop) Receipt { return s.Checkout("bob", "book", 2000) })
	if got != (Receipt{true, "ch_42"}) || len(calls) != 1 ||
		calls[0] != "POST https://api.stripe.com/v1/charges customer=bob&amount=2000" ||
		sent[0] != "bob: paid, receipt ch_42" || d.qty["book"] != 0 || len(d.seen) != 1 {
		t.Fatalf("%v %v %v %v", got, calls, sent, d)
	}
}

func TestOutOfStockNeverCharges(t *testing.T) {
	var calls, sent []string
	db, _ := open(map[string]int{"book": 0})
	app := Production(db, &calls, &sent)
	got := app(func(s Shop) Receipt { return s.Checkout("eve", "book", 2000) })
	if got.Paid || !strings.Contains(got.Detail, "out of stock") || len(calls) != 0 {
		t.Fatalf("%v %v", got, calls)
	}
}

// a failed statement is raised where Stock performed it: Checkout does not go on to charge
func TestADatabaseFailureStopsTheCheckout(t *testing.T) {
	var sent []string
	app := func(use func(Shop) Receipt) Receipt { // the test assembly, its Table refusing everything
		return okay.Main(func(r *okay.Run) Receipt {
			return Mailbox(r, &sent, func(mail Notify) Receipt {
				return Card(r, true, func(pay Payments) Receipt {
					return Sql(r, brokenDb(), func(db Db) Receipt {
						return Stock(r, db, func(inv Inventory) Receipt { return use(Shop{inv, pay, mail}) })
					})
				})
			})
		})
	}
	defer func() {
		if p := recover(); p == nil || !strings.Contains(p.(error).Error(), "database is down") || len(sent) != 0 {
			t.Fatalf("want the failure raised before any mail, got %v %v", p, sent)
		}
	}()
	app(func(s Shop) Receipt { return s.Checkout("bob", "book", 2000) })
}
