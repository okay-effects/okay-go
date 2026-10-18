// Package wire: okay-go's steps over a wire. Each side sees only operations and
// answers — an operation is its name and one record of its fields (keys in
// lowerCamel, as a Scala case class's), an answer a plain value (numbers,
// strings, lists, maps) decoded into what the operation answers.
// No dependency on any transport: a Caller is whatever sends a request and waits.
package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode"

	okay "github.com/okay-effects/okay-go"
)

// Caller: one request over the wire, its answer, or the failure the other side raised.
type Caller func(name string, args []any) (any, error)

// Named: an operation that names itself; otherwise its type's name in snake_case (PriceOf: price_of).
type Named interface{ OpName() string }

type answering interface{ AnswerType() reflect.Type }

// Name of an operation on the wire.
func Name(op any) string {
	if n, ok := op.(Named); ok {
		return n.OpName()
	}
	return snake(reflect.TypeOf(op).Name())
}

// Encode: an operation as its name and one record of its fields (the Answers
// marker left out); none for an operation without fields.
func Encode(op any) (string, []any) {
	v := reflect.ValueOf(op)
	rec := map[string]any{}
	for i := 0; i < v.NumField(); i++ {
		if f := v.Type().Field(i); f.IsExported() && !f.Anonymous {
			x, err := plain(v.Field(i).Interface())
			if err != nil {
				panic(err)
			}
			rec[lowerCamel(f.Name)] = x
		}
	}
	if len(rec) == 0 {
		return Name(op), nil
	}
	return Name(op), []any{rec}
}

// Decode: a plain answer into what op answers.
func Decode(op any, answer any) (any, error) {
	a, ok := op.(answering)
	if !ok {
		return nil, fmt.Errorf("okay-go wire: %T does not embed okay.Answers", op)
	}
	return into(a.AnswerType(), answer)
}

// Remote: every operation answered over the wire — sent through call as its
// name and record, its plain answer decoded into what the operation answers.
// Middleware goes around it as around any okay.Answerer.
func Remote(call Caller) okay.Answerer {
	return func(op any) (any, error) {
		name, args := Encode(op)
		answer, err := call(name, args)
		if err != nil {
			return nil, err
		}
		return Decode(op, answer)
	}
}

// Drive: a handed-out program run to its value, every operation it performs
// for the host sent through call. A failure the other side raised is raised
// where the operation was performed; a failure of the program is the error.
func Drive[A any](c *okay.Computation[A], call Caller) (A, error) {
	return okay.Drive(c, Remote(call))
}

// Serve: the other way — a Go answer to O, callable from the wire by name with
// O's record as its argument, its answer a plain value.
func Serve[O okay.Op[R], R any](answer func(O) R) (string, func(args []any) (any, error)) {
	var zero O
	return Name(zero), func(args []any) (any, error) {
		var rec any = map[string]any{}
		if len(args) == 1 {
			rec = args[0]
		} else if len(args) > 1 {
			return nil, fmt.Errorf("okay-go wire: %s takes one record, got %d arguments", Name(zero), len(args))
		}
		op, err := into(reflect.TypeFor[O](), rec)
		if err != nil {
			return nil, err
		}
		return plain(answer(op.(O)))
	}
}

// into: v as t — itself if it already is one, else through JSON (a map into a struct, int64 into int).
func into(t reflect.Type, v any) (any, error) {
	if t.Kind() == reflect.Struct && t.NumField() == 0 {
		return reflect.Zero(t).Interface(), nil // nothing to answer: whatever came says only "done"
	}
	if v != nil && reflect.TypeOf(v).AssignableTo(t) {
		return v, nil
	}
	p := reflect.New(t)
	bs, err := json.Marshal(v)
	if err == nil {
		err = json.Unmarshal(bs, p.Interface())
	}
	if err != nil {
		return nil, fmt.Errorf("okay-go wire: %v as %v: %w", v, t, err)
	}
	return p.Elem().Interface(), nil
}

// plain: a Go answer as wire values — a struct as a map of its fields
func plain(v any) (any, error) {
	switch reflect.ValueOf(v).Kind() {
	case reflect.Struct, reflect.Map, reflect.Slice:
		bs, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		d := json.NewDecoder(bytes.NewReader(bs))
		d.UseNumber()
		var out any
		if err := d.Decode(&out); err != nil {
			return nil, err
		}
		return numbers(out), nil
	}
	return v, nil
}

// numbers: JSON's numbers as int64 where whole, float64 otherwise — as the wire has them
func numbers(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		f, _ := x.Float64()
		return f
	case []any:
		for i := range x {
			x[i] = numbers(x[i])
		}
	case map[string]any:
		for k := range x {
			x[k] = numbers(x[k])
		}
	}
	return v
}

func lowerCamel(s string) string {
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

func snake(s string) string {
	var b strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) {
			if i > 0 {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}
