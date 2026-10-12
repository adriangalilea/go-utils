package stream

import (
	"bytes"
	"strings"
	"testing"
)

// An error passed up through spans prints its stack once: under the span
// that failed with it first, the message alone on every span it is passed up
// to. A hidden span's fail line counts as that first failure, and a different
// error still prints its own stack.
func TestHumanStackOnce(t *testing.T) {
	const (
		trace = `"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`
		err   = `"err":{"type":"Error","message":"boom","stack":"Error: boom\n    at charge (app.ts:1)"}`
		other = `"err":{"type":"Error","message":"bang","stack":"Error: bang\n    at refund (app.ts:9)"}`
		head  = `{"time":"2026-10-12T12:20:39.123Z","level":`
	)
	lines := []string{
		head + `"info","verb":"wait","msg":"turn","service":"s","phase":"start",` + trace + `,"span_id":"00f067aa0ba902b7"}`,
		head + `"info","verb":"wait","msg":"charge","service":"s","phase":"start",` + trace + `,"span_id":"00f067aa0ba902b8","parent_id":"00f067aa0ba902b7"}`,
		head + `"error","verb":"fail","msg":"charge","service":"s","phase":"end",` + trace + `,"span_id":"00f067aa0ba902b8","parent_id":"00f067aa0ba902b7","status":"error","dur_ms":1,"fields":{` + err + `}}`,
		head + `"error","verb":"fail","msg":"turn","service":"s","phase":"end",` + trace + `,"span_id":"00f067aa0ba902b7","status":"error","dur_ms":2,"fields":{` + err + `}}`,
		head + `"info","verb":"wait","msg":"turn","service":"s","phase":"start",` + trace + `,"span_id":"00f067aa0ba902c7"}`,
		head + `"error","verb":"fail","msg":"refund","service":"s",` + trace + `,"span_id":"00f067aa0ba902c7","fields":{"dur_ms":1,` + other + `}}`,
		head + `"error","verb":"fail","msg":"turn","service":"s","phase":"end",` + trace + `,"span_id":"00f067aa0ba902c7","status":"error","dur_ms":2,"fields":{` + other + `}}`,
		head + `"error","verb":"fail","msg":"turn","service":"s","phase":"end",` + trace + `,"span_id":"00f067aa0ba902d7","status":"error","dur_ms":2,"fields":{` + err + `}}`,
	}
	var buf bytes.Buffer
	h := NewHuman(&buf)
	var got strings.Builder
	for _, line := range lines {
		e, ok := Parse([]byte(line))
		if !ok {
			t.Fatalf("not an event: %s", line)
		}
		got.WriteString(h.Render(&e, false))
	}
	want := `○ turn
  ○ charge
  ⨯ charge 1.0ms err=boom
      Error: boom
          at charge (app.ts:1)
⨯ turn 2.0ms err=boom
○ turn
  ⨯ refund dur_ms=1 err=bang
      Error: bang
          at refund (app.ts:9)
⨯ turn 2.0ms err=bang
⨯ turn 2.0ms err=boom
    Error: boom
        at charge (app.ts:1)
`
	if got.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", got.String(), want)
	}
}
