package okay.foreign

import java.nio.file.{Files, Path}
import okay.codec.Schema
import okay.std.Reader

/** okay-go (a Go effects library, its own capabilities and handlers) over
 * okay's worker wire: each side sees only operations and answers */
object TestOkayGoWire:
  final case class Reserve(item: String) derives Schema
  final case class Charge(user: String, cents: Long) derives Schema
  final case class Email(to: String, text: String) derives Schema
  final case class Receipt(paid: Boolean, detail: String) derives Schema
  final case class Store(stock: Set[String], approve: Boolean)

  val sent = scala.collection.mutable.ArrayBuffer[String]()
  val reserve = Foreign.callback[Reserve, Boolean]("reserve")(r => Reader.ask[Store].map(_.stock(r.item)))
  val charge = Foreign.callback[Charge, Receipt]("charge")(c =>
    Reader.ask[Store].map(s => if s.approve then Receipt(true, s"scala-${c.user}-${c.cents}") else Receipt(false, "declined")))
  val email = Foreign.callback[Email, Boolean]("email")(e => Reader.ask[Store].map { _ => sent += s"${e.to}: ${e.text}"; true })

  val main: String = """package main

import (
	"fmt"

	okaygo "github.com/okay-effects/okay-go"
	"github.com/okay-effects/okay-go/examples/shop"
	"github.com/okay-effects/okay-go/wire"
	"worker/okay"
)

// okay's ordered dicts as the plain maps package wire reads
func plain(v any) any {
	switch x := v.(type) {
	case okay.Dict:
		m := map[string]any{}
		for _, kv := range x {
			m[kv.Key] = plain(kv.Val)
		}
		return m
	case []any:
		for i := range x {
			x[i] = plain(x[i])
		}
	}
	return v
}

// the Go shop, unchanged, handed out: its capabilities answered by okay, step by step
func checkout(c *okay.Ctx, args []any) any {
	user, item, cents := args[0].(string), args[1].(string), args[2].(int64)
	run := okaygo.Start(func(r *okaygo.Run, h okaygo.Host) shop.Receipt {
		return shop.Remote(h).Checkout(user, item, cents)
	})
	receipt, err := wire.Drive(run, func(name string, args []any) (any, error) {
		v, err := okay.Call(c, okay.Named(name, args...))
		return plain(v), err
	})
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%v %s", receipt.Paid, receipt.Detail)
}

// a Go handler, served to okay by its operation's name
func served[O okaygo.Op[R], R any](fs okay.Functions, answer func(O) R) {
	name, f := wire.Serve(answer)
	fs[name] = func(_ *okay.Ctx, args []any) any {
		v, err := f(plain(args).([]any))
		if err != nil {
			panic(err)
		}
		return v
	}
}

func main() {
	fs := okay.Functions{"checkout": checkout}
	served(fs, func(op shop.Charge) shop.Receipt {
		return shop.Receipt{Paid: op.Cents <= 5000, Detail: fmt.Sprintf("go-%s-%d", op.User, op.Cents)}
	})
	okay.Main(okay.Programs{}, fs)
}
"""

class TestOkayGoWire extends munit.FunSuite {
  import TestOkayGoWire.*

  override def munitTests(): Seq[Test] = super.munitTests().map(_.tag(new munit.Tag("Live")))
  private val okayGo = Path.of("/Users/sergiy/work/my/okay-go")

  private lazy val w =
    val dir = Files.createTempDirectory("okay-go-wire")
    Files.writeString(dir.resolve("go.mod"),
      s"module worker\n\ngo 1.27\n\nrequire github.com/okay-effects/okay-go v0.0.0\n\nreplace github.com/okay-effects/okay-go => $okayGo\n"): Unit
    Files.writeString(dir.resolve("main.go"), main): Unit
    ForeignWorker.speaking(Seq(GoWorker.build(dir).toString))
  private given okay.Answers[ForeignEval] = w.handler
  override def afterAll(): Unit = w.close()

  private val cbs = Foreign.callbacks(reserve, charge, email)

  test("the Go shop runs in Go; its inventory, payments and mail are Scala handlers, step by step over the wire") {
    sent.clear()
    val paid = Foreign.fn[String]("checkout").calling(cbs)("bob", "book", 2000L)
    assertEquals(Reader.run(Store(Set("book"), approve = true))(paid).runWith, Right("true scala-bob-2000"))
    val out = Foreign.fn[String]("checkout").calling(cbs)("eve", "pen", 100L)
    assertEquals(Reader.run(Store(Set("book"), approve = true))(out).runWith, Right("false pen: out of stock"))
    assertEquals(sent.toList, List("bob: paid, receipt scala-bob-2000", "eve: not paid: pen: out of stock"))
  }

  test("the other way: a Go handler answers okay's operation, a record in, a record out") {
    assertEquals(Foreign.fn[Receipt]("charge")(Charge("ann", 300L)).runWith, Right(Receipt(true, "go-ann-300")))
    assertEquals(Foreign.fn[Receipt]("charge")(Charge("ann", 9000L)).runWith, Right(Receipt(false, "go-ann-9000")))
  }
}
