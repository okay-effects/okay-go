package wire_test

import (
	"fmt"

	okay "github.com/okay-effects/okay-go"
	"github.com/okay-effects/okay-go/examples/shop"
	"github.com/okay-effects/okay-go/wire"
)

// The shop stepped over a wire: the far side sees a name and a record, answers plain values.
func ExampleDrive() {
	run := okay.Start(func(r *okay.Run, h okay.Host) shop.Receipt {
		return shop.Remote(h).Checkout("bob", "book", 2000)
	})
	receipt, err := wire.Drive(run, func(name string, args []any) (any, error) {
		fmt.Println(name, args)
		switch name {
		case "reserve":
			return true, nil
		case "charge":
			return map[string]any{"paid": true, "detail": "ch_42"}, nil
		}
		return nil, nil
	})
	fmt.Println(receipt, err)
	// Output:
	// reserve [map[item:book]]
	// charge [map[cents:2000 user:bob]]
	// email [map[text:paid, receipt ch_42 to:bob]]
	// {true ch_42} <nil>
}
