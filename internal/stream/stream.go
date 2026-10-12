// Package stream is the event of the logging contract
// (github.com/adriangalilea/utils): its levels and verbs, the JSON line in the
// contract's key order, the parse of such a line back into an event, and the
// two renderings a person reads (human, grep line). The logger in go-utils
// writes through it and the logs viewer reads through it, so what one writes
// the other renders the same way.
package stream

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"time"
	"unicode/utf8"
)

// Level orders events for filtering: an event shows when its level is at or
// below the threshold. Silent is a threshold only, never an event's level.
type Level uint8

const (
	LevelSilent Level = iota
	LevelError
	LevelWarn
	LevelInfo
	LevelDebug
	LevelTrace
)

var levelNames = [...]string{"silent", "error", "warn", "info", "debug", "trace"}

func (l Level) String() string { return levelNames[l] }

// ParseLevel accepts exactly the contract's spellings.
func ParseLevel(s string) (Level, bool) {
	for i, name := range levelNames {
		if s == name {
			return Level(i), true
		}
	}
	return 0, false
}

// Verb says what happened; each prints at a fixed level.
type Verb uint8

const (
	VerbError Verb = iota
	VerbFail
	VerbWarn
	VerbInfo
	VerbSuccess
	VerbWait
	VerbReady
	VerbStep
	VerbDebug
	VerbTrace
)

type verbSpec struct {
	name   string
	level  Level
	symbol string
}

var verbs = [...]verbSpec{
	VerbError:   {"error", LevelError, "⨯"},
	VerbFail:    {"fail", LevelError, "⨯"},
	VerbWarn:    {"warn", LevelWarn, "⚠"},
	VerbInfo:    {"info", LevelInfo, " "},
	VerbSuccess: {"success", LevelInfo, "✓"},
	VerbWait:    {"wait", LevelInfo, "○"},
	VerbReady:   {"ready", LevelInfo, "▶"},
	VerbStep:    {"step", LevelInfo, "•"},
	VerbDebug:   {"debug", LevelDebug, "◦"},
	VerbTrace:   {"trace", LevelTrace, "»"},
}

func (v Verb) String() string { return verbs[v].name }
func (v Verb) Level() Level   { return verbs[v].level }

func parseVerb(s string) (Verb, bool) {
	for i, spec := range verbs {
		if s == spec.name {
			return Verb(i), true
		}
	}
	return 0, false
}

// Phase marks a span's own events; a line has none.
type Phase uint8

const (
	PhaseNone Phase = iota
	PhaseStart
	PhaseEnd
)

// Field is one entry of an event's fields: the key and its value already in
// JSON, so writing and reading an event carry the same bytes.
type Field struct {
	Key   string
	Value json.RawMessage
}

// Event is one event of the stream. Level is the verb's; status is the end
// verb's (success: ok, fail: error); DurMs is meaningful only on PhaseEnd.
type Event struct {
	Time     string
	Verb     Verb
	Msg      string
	Service  string
	Scope    []string
	Phase    Phase
	TraceID  string
	SpanID   string
	ParentID string
	DurMs    float64
	Fields   []Field
}

// TimeLayout is the event instant: UTC, RFC3339 with milliseconds.
const TimeLayout = "2006-01-02T15:04:05.000Z"

func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

func (e *Event) status() string {
	if e.Verb == VerbFail {
		return "error"
	}
	return "ok"
}

// AppendJSON writes the event as its JSON line, without the newline: keys in
// the contract's order, each only when it has a value.
func (e *Event) AppendJSON(b []byte) []byte {
	b = append(b, `{"time":`...)
	b = AppendString(b, e.Time)
	b = append(b, `,"level":`...)
	b = AppendString(b, e.Verb.Level().String())
	b = append(b, `,"verb":`...)
	b = AppendString(b, e.Verb.String())
	b = append(b, `,"msg":`...)
	b = AppendString(b, e.Msg)
	b = append(b, `,"service":`...)
	b = AppendString(b, e.Service)
	if len(e.Scope) > 0 {
		b = append(b, `,"scope":[`...)
		for i, s := range e.Scope {
			if i > 0 {
				b = append(b, ',')
			}
			b = AppendString(b, s)
		}
		b = append(b, ']')
	}
	switch e.Phase {
	case PhaseStart:
		b = append(b, `,"phase":"start"`...)
	case PhaseEnd:
		b = append(b, `,"phase":"end"`...)
	}
	if e.TraceID != "" {
		b = append(b, `,"trace_id":`...)
		b = AppendString(b, e.TraceID)
	}
	if e.SpanID != "" {
		b = append(b, `,"span_id":`...)
		b = AppendString(b, e.SpanID)
	}
	if e.ParentID != "" {
		b = append(b, `,"parent_id":`...)
		b = AppendString(b, e.ParentID)
	}
	if e.Phase == PhaseEnd {
		b = append(b, `,"status":`...)
		b = AppendString(b, e.status())
		b = append(b, `,"dur_ms":`...)
		b = AppendNumber(b, e.DurMs, 64)
	}
	if len(e.Fields) > 0 {
		b = append(b, `,"fields":{`...)
		for i, f := range e.Fields {
			if i > 0 {
				b = append(b, ',')
			}
			b = AppendString(b, f.Key)
			b = append(b, ':')
			b = append(b, f.Value...)
		}
		b = append(b, '}')
	}
	return append(b, '}')
}

const hexDigits = "0123456789abcdef"

// AppendString writes s as a JSON string the way JavaScript's JSON.stringify
// does, so the three libraries write the same bytes: only the quote, the
// backslash and control characters are escaped; invalid UTF-8 becomes U+FFFD.
func AppendString(b []byte, s string) []byte {
	b = append(b, '"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\':
				b = append(b, '\\', c)
			case c == '\n':
				b = append(b, '\\', 'n')
			case c == '\r':
				b = append(b, '\\', 'r')
			case c == '\t':
				b = append(b, '\\', 't')
			case c == '\b':
				b = append(b, '\\', 'b')
			case c == '\f':
				b = append(b, '\\', 'f')
			case c < 0x20:
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
			default:
				b = append(b, c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, "�"...)
		} else {
			b = append(b, s[i:i+size]...)
		}
		i += size
	}
	return append(b, '"')
}

// AppendNumber writes a float the way JavaScript prints a number (the
// algorithm encoding/json uses): shortest round-trip digits, exponent form
// only below 1e-6 or from 1e21. NaN and the infinities are not JSON numbers;
// the contract writes them as the strings "NaN", "Infinity", "-Infinity".
// Negative zero is 0, as JavaScript writes it.
func AppendNumber(b []byte, f float64, bits int) []byte {
	switch {
	case math.IsNaN(f):
		return append(b, `"NaN"`...)
	case math.IsInf(f, 1):
		return append(b, `"Infinity"`...)
	case math.IsInf(f, -1):
		return append(b, `"-Infinity"`...)
	case f == 0:
		return append(b, '0')
	}
	format := byte('f')
	abs := math.Abs(f)
	if bits == 64 && (abs < 1e-6 || abs >= 1e21) || bits == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
		format = 'e'
	}
	b = strconv.AppendFloat(b, f, format, -1, bits)
	if format == 'e' {
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}

// Parse reads one line as an event. A line that is not exactly an event in
// the contract's shape (any JSON that breaks the schema, any text) is not one:
// the caller passes it through as it is.
func Parse(line []byte) (Event, bool) {
	var r struct {
		Time     *string         `json:"time"`
		Level    *string         `json:"level"`
		Verb     *string         `json:"verb"`
		Msg      *string         `json:"msg"`
		Service  *string         `json:"service"`
		Scope    []string        `json:"scope"`
		Phase    string          `json:"phase"`
		TraceID  string          `json:"trace_id"`
		SpanID   string          `json:"span_id"`
		ParentID string          `json:"parent_id"`
		Status   string          `json:"status"`
		DurMs    *float64        `json:"dur_ms"`
		Fields   json.RawMessage `json:"fields"`
	}
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return Event{}, false
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) != nil {
		return Event{}, false
	}
	if _, err := dec.Token(); err != io.EOF {
		return Event{}, false
	}
	if r.Time == nil || r.Level == nil || r.Verb == nil || r.Msg == nil || r.Service == nil || *r.Service == "" {
		return Event{}, false
	}
	if _, err := time.Parse(TimeLayout, *r.Time); err != nil {
		return Event{}, false
	}
	verb, ok := parseVerb(*r.Verb)
	if !ok || verb.Level().String() != *r.Level {
		return Event{}, false
	}
	if r.Scope != nil && len(r.Scope) == 0 {
		return Event{}, false
	}
	e := Event{
		Time: *r.Time, Verb: verb, Msg: *r.Msg, Service: *r.Service, Scope: r.Scope,
		TraceID: r.TraceID, SpanID: r.SpanID, ParentID: r.ParentID,
	}
	if !isHex(e.TraceID, 32) || !isHex(e.SpanID, 16) || !isHex(e.ParentID, 16) {
		return Event{}, false
	}
	if e.SpanID != "" && e.TraceID == "" {
		return Event{}, false
	}
	switch r.Phase {
	case "":
		if e.ParentID != "" || r.Status != "" || r.DurMs != nil {
			return Event{}, false
		}
	case "start":
		e.Phase = PhaseStart
		if verb != VerbWait || r.Status != "" || r.DurMs != nil || e.SpanID == "" {
			return Event{}, false
		}
	case "end":
		e.Phase = PhaseEnd
		if (verb != VerbSuccess && verb != VerbFail) || r.DurMs == nil || *r.DurMs < 0 || e.SpanID == "" {
			return Event{}, false
		}
		e.DurMs = *r.DurMs
		if r.Status != e.status() {
			return Event{}, false
		}
	default:
		return Event{}, false
	}
	if r.Fields != nil {
		fields, ok := ParseObject(r.Fields)
		if !ok {
			return Event{}, false
		}
		e.Fields = fields
	}
	return e, true
}

// isHex: empty (absent), or exactly n lowercase hex digits.
func isHex(s string, n int) bool {
	if s == "" {
		return true
	}
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ParseObject reads a JSON object into its entries in the order written,
// each value compacted. Not an object: false.
func ParseObject(raw json.RawMessage) ([]Field, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false
	}
	var fields []Field
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, false
		}
		key := t.(string)
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return nil, false
		}
		var compact bytes.Buffer
		if json.Compact(&compact, value) != nil {
			return nil, false
		}
		fields = append(fields, Field{Key: key, Value: compact.Bytes()})
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, false
	}
	return fields, true
}
