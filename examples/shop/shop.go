// Package shop: an application as capabilities — the same shop as in okay's Scala and Java examples.
package shop

import (
	"database/sql"
	"fmt"

	okay "github.com/okay-effects/okay-go"
)

// ---- the services: operations, and their typed faces over a capability

type Reserve struct {
	okay.Answers[bool]
	Item string
}
type Charge struct {
	okay.Answers[Receipt]
	User  string
	Cents int64
}
type Email struct {
	okay.Answers[struct{}]
	To, Text string
}
type Receipt struct {
	Paid   bool   `json:"paid"`
	Detail string `json:"detail"`
}

type Inventory struct{ cap okay.Cap[Reserve] }

func (i Inventory) Reserve(item string) bool { return okay.Perform(i.cap, Reserve{Item: item}) }

type Payments struct{ cap okay.Cap[Charge] }

func (p Payments) Charge(user string, cents int64) Receipt {
	return okay.Perform(p.cap, Charge{User: user, Cents: cents})
}

type Notify struct{ cap okay.Cap[Email] }

func (n Notify) Email(to, text string) { okay.Perform(n.cap, Email{To: to, Text: text}) }

// ---- the business code: a plain struct, its dependencies capabilities, plain Go

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

// ---- handlers: each makes a capability and hands it to its body

// the inventory over a database: Reserve becomes one statement, answered by whatever Db is
const ReserveSQL = "update stock set qty = qty - 1 where sku = ? and qty > 0"

func Stock[A any](r *okay.Run, db Db, body func(Inventory) A) A {
	return okay.Answer(r, func(op Reserve) bool {
		return db.Exec(ReserveSQL, op.Item) == 1 // a row updated: one was there, now it is ours
	}, func(c okay.Cap[Reserve]) A { return body(Inventory{c}) })
}

func Mailbox[A any](r *okay.Run, sent *[]string, body func(Notify) A) A {
	return okay.Answer(r, func(op Email) struct{} {
		*sent = append(*sent, op.To+": "+op.Text)
		return struct{}{}
	}, func(c okay.Cap[Email]) A { return body(Notify{c}) })
}

// for tests: payments without any HTTP
func Card[A any](r *okay.Run, approve bool, body func(Payments) A) A {
	return okay.Answer(r, func(op Charge) Receipt {
		if approve {
			return Receipt{true, "test-" + op.User}
		}
		return Receipt{false, "declined"}
	}, func(c okay.Cap[Charge]) A { return body(Payments{c}) })
}

// ---- a database, as an effect: a statement, its rows affected

type Exec struct {
	okay.Answers[int64]
	SQL  string
	Args []any
}
type Db struct{ cap okay.Cap[Exec] }

func (d Db) Exec(sql string, args ...any) int64 {
	return okay.Perform(d.cap, Exec{SQL: sql, Args: args})
}

// production: database/sql; a failed statement is raised where it was performed
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

// for tests: the stock table in memory, answering the statements the shop makes
func Table[A any](r *okay.Run, stock map[string]int, body func(Db) A) A {
	return okay.Handle(r, func(op Exec) any {
		if op.SQL != ReserveSQL {
			return okay.Raise(fmt.Errorf("table: no such statement: %s", op.SQL))
		}
		sku := op.Args[0].(string)
		if stock[sku] == 0 {
			return int64(0)
		}
		stock[sku]--
		return int64(1)
	}, func(c okay.Cap[Exec]) A { return body(Db{c}) })
}

// a component built on another: payments over HTTP

type Post struct {
	okay.Answers[Response]
	URL, Body string
}
type Response struct {
	Status int
	Body   string
}
type Http struct{ cap okay.Cap[Post] }

func (h Http) Post(url, body string) Response { return okay.Perform(h.cap, Post{URL: url, Body: body}) }

func Gateway[A any](r *okay.Run, calls *[]string, body func(Http) A) A {
	return okay.Answer(r, func(op Post) Response {
		*calls = append(*calls, "POST "+op.URL+" "+op.Body)
		return Response{200, "ch_42"}
	}, func(c okay.Cap[Post]) A { return body(Http{c}) })
}

func Stripe[A any](r *okay.Run, http Http, body func(Payments) A) A {
	return okay.Answer(r, func(op Charge) Receipt {
		res := http.Post("https://api.stripe.com/v1/charges", fmt.Sprintf("customer=%s&amount=%d", op.User, op.Cents))
		if res.Status == 200 {
			return Receipt{true, res.Body}
		}
		return Receipt{false, fmt.Sprintf("card declined (%d): %s", res.Status, res.Body)}
	}, func(c okay.Cap[Charge]) A { return body(Payments{c}) })
}

// ---- the system: every capability made, a Shop handed to what uses it

type App func(use func(Shop) Receipt) Receipt

func Production(db *sql.DB, calls, sent *[]string) App {
	return func(use func(Shop) Receipt) Receipt {
		return okay.Main(func(r *okay.Run) Receipt {
			return Mailbox(r, sent, func(mail Notify) Receipt {
				return Gateway(r, calls, func(http Http) Receipt {
					return Stripe(r, http, func(pay Payments) Receipt {
						return Sql(r, db, func(db Db) Receipt {
							return Stock(r, db, func(inv Inventory) Receipt {
								return use(Shop{inv, pay, mail})
							})
						})
					})
				})
			})
		})
	}
}

func Test(stock map[string]int, approve bool, sent *[]string) App {
	return func(use func(Shop) Receipt) Receipt {
		return okay.Main(func(r *okay.Run) Receipt {
			return Mailbox(r, sent, func(mail Notify) Receipt {
				return Card(r, approve, func(pay Payments) Receipt {
					return Table(r, stock, func(db Db) Receipt {
						return Stock(r, db, func(inv Inventory) Receipt {
							return use(Shop{inv, pay, mail})
						})
					})
				})
			})
		})
	}
}

// ---- the shop handed out: its capabilities answered by whoever steps it (okay.Start, package wire)

func Remote(h okay.Host) Shop {
	return Shop{Inventory{okay.Outside[Reserve](h)}, Payments{okay.Outside[Charge](h)}, Notify{okay.Outside[Email](h)}}
}
