package utils

import (
	"bytes"
	"cmp"
	"encoding"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/adriangalilea/go-utils/internal/stream"
)

// encodeFields turns F into an event's fields: keys sorted, every value
// written by the contract's table, so the same value reads the same from Go,
// TypeScript and Python.
func encodeFields(f F) []stream.Field {
	if len(f) == 0 {
		return nil
	}
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	out := make([]stream.Field, len(keys))
	for i, k := range keys {
		out[i] = stream.Field{Key: k, Value: appendValue(nil, f[k], nil)}
	}
	return out
}

// appendValue writes v as JSON by the contract's table:
//
//	string, finite number, bool, nil    itself
//	NaN, ±Inf                           "NaN", "Infinity", "-Infinity"
//	error                               {type, message, cause, source…}
//	time.Time                           the event-time string
//	time.Duration                       milliseconds, 3 decimals
//	[]byte                              base64, as encoding/json writes it
//	*big.Int, *big.Float, *big.Rat      their decimal string
//	maps with string keys, slices       recursively, map keys sorted
//	structs, other maps, Marshalers     encoding/json, then keys sorted
//	a map, slice or pointer that holds  "[circular]"
//	  one of its own ancestors
//	what JSON cannot hold (chan, func,  fmt.Sprint
//	  complex) or encoding/json refuses
//
// seen is the path of maps, slices and pointers from the field down to v.
func appendValue(b []byte, v any, seen []uintptr) []byte {
	switch x := v.(type) {
	case nil:
		return append(b, "null"...)
	case error:
		return appendError(b, x, seen)
	case time.Time:
		return stream.AppendString(b, stream.FormatTime(x))
	case time.Duration:
		return stream.AppendNumber(b, math.Round(float64(x)/1e3)/1e3, 64)
	case []byte:
		return stream.AppendString(b, base64.StdEncoding.EncodeToString(x))
	case *big.Int, *big.Float, *big.Rat:
		return stream.AppendString(b, fmt.Sprint(x))
	case json.Marshaler, encoding.TextMarshaler:
		return appendViaJSON(b, v)
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return stream.AppendString(b, rv.String())
	case reflect.Bool:
		return strconv.AppendBool(b, rv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.AppendInt(b, rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.AppendUint(b, rv.Uint(), 10)
	case reflect.Float32:
		return stream.AppendNumber(b, rv.Float(), 32)
	case reflect.Float64:
		return stream.AppendNumber(b, rv.Float(), 64)
	case reflect.Map:
		if rv.IsNil() {
			return append(b, "null"...)
		}
		if rv.Type().Key().Kind() != reflect.String {
			return appendViaJSON(b, v)
		}
		if slices.Contains(seen, rv.Pointer()) {
			return stream.AppendString(b, "[circular]")
		}
		seen = append(seen, rv.Pointer())
		keys := rv.MapKeys()
		slices.SortFunc(keys, func(a, c reflect.Value) int { return cmp.Compare(a.String(), c.String()) })
		b = append(b, '{')
		for i, k := range keys {
			if i > 0 {
				b = append(b, ',')
			}
			b = stream.AppendString(b, k.String())
			b = append(b, ':')
			b = appendValue(b, rv.MapIndex(k).Interface(), seen)
		}
		return append(b, '}')
	case reflect.Slice:
		if rv.IsNil() {
			return append(b, "null"...)
		}
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return stream.AppendString(b, base64.StdEncoding.EncodeToString(rv.Bytes()))
		}
		if slices.Contains(seen, rv.Pointer()) {
			return stream.AppendString(b, "[circular]")
		}
		return appendElements(b, rv, append(seen, rv.Pointer()))
	case reflect.Array:
		return appendElements(b, rv, seen)
	case reflect.Pointer:
		if rv.IsNil() {
			return append(b, "null"...)
		}
		if slices.Contains(seen, rv.Pointer()) {
			return stream.AppendString(b, "[circular]")
		}
		if rv.Elem().Kind() == reflect.Struct {
			return appendViaJSON(b, v)
		}
		return appendValue(b, rv.Elem().Interface(), append(seen, rv.Pointer()))
	case reflect.Struct:
		return appendViaJSON(b, v)
	}
	return stream.AppendString(b, fmt.Sprint(v))
}

func appendElements(b []byte, rv reflect.Value, seen []uintptr) []byte {
	b = append(b, '[')
	for i := range rv.Len() {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendValue(b, rv.Index(i).Interface(), seen)
	}
	return append(b, ']')
}

// appendError writes an error as the contract's error object: type (the Go
// type, as %T prints it), message, the errors.Unwrap chain as cause, and a
// SourcedError's source, operation, status and context, in that order. The
// message is the one the error was given: Error() for a Go error, Message for
// a SourcedError, whose source and operation are keys of their own.
func appendError(b []byte, err error, seen []uintptr) []byte {
	s, sourced := err.(*SourcedError)
	message := err.Error()
	if sourced {
		message = s.Message
	}
	b = append(b, `{"type":`...)
	b = stream.AppendString(b, reflect.TypeOf(err).String())
	b = append(b, `,"message":`...)
	b = stream.AppendString(b, message)
	if cause := errors.Unwrap(err); cause != nil {
		b = append(b, `,"cause":`...)
		b = appendError(b, cause, seen)
	}
	if sourced {
		b = append(b, `,"source":`...)
		b = stream.AppendString(b, s.Source)
		b = append(b, `,"operation":`...)
		b = stream.AppendString(b, s.Operation)
		if s.Status != 0 {
			b = append(b, `,"status":`...)
			b = strconv.AppendInt(b, int64(s.Status), 10)
		}
		if len(s.Context) > 0 {
			b = append(b, `,"context":`...)
			b = appendValue(b, s.Context, seen)
		}
	}
	return append(b, '}')
}

// appendViaJSON encodes v with encoding/json (struct tags, Marshalers) and
// writes the result back with every object's keys sorted. A value
// encoding/json refuses (a cycle, a channel inside, NaN in a struct) is
// written as its fmt.Sprint string.
func appendViaJSON(b []byte, v any) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return stream.AppendString(b, fmt.Sprint(v))
	}
	dec := json.NewDecoder(&buf)
	dec.UseNumber()
	var tree any
	Check(dec.Decode(&tree), "re-reading what encoding/json wrote")
	return appendTree(b, tree)
}

// appendTree writes a decoded JSON tree: objects with sorted keys, numbers
// exactly as encoding/json wrote them.
func appendTree(b []byte, tree any) []byte {
	switch x := tree.(type) {
	case nil:
		return append(b, "null"...)
	case bool:
		return strconv.AppendBool(b, x)
	case json.Number:
		return append(b, x...)
	case string:
		return stream.AppendString(b, x)
	case []any:
		b = append(b, '[')
		for i, e := range x {
			if i > 0 {
				b = append(b, ',')
			}
			b = appendTree(b, e)
		}
		return append(b, ']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		b = append(b, '{')
		for i, k := range keys {
			if i > 0 {
				b = append(b, ',')
			}
			b = stream.AppendString(b, k)
			b = append(b, ':')
			b = appendTree(b, x[k])
		}
		return append(b, '}')
	}
	panic(&Panic{Message: String("encoding/json decoded an unexpected type:", reflect.TypeOf(tree))})
}
