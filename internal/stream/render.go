package stream

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// Human renders events for a person at a terminal: the verb's symbol, the
// scope dim in brackets, the message, fields dim as key=value, an error's
// stack dim under its line, and a span as `○ name` … `✓ name 254ms` with what
// it holds indented one step deeper. Indentation comes from the stream itself
// (parent_id, span_id), so the logger and the viewer agree, and an event whose
// span never showed sits at the depth of the nearest one that did.
type Human struct {
	depth   map[string]int
	symbols [len(verbs)]lipgloss.Style
	dim     lipgloss.Style
}

// NewHuman styles for w: colour when w is a terminal that takes it (lipgloss
// reads its profile and NO_COLOR), plain text otherwise.
func NewHuman(w io.Writer) *Human {
	r := lipgloss.NewRenderer(w)
	bold := func(color string) lipgloss.Style { return r.NewStyle().Bold(true).Foreground(lipgloss.Color(color)) }
	dim := r.NewStyle().Foreground(lipgloss.Color("8"))
	return &Human{
		depth: make(map[string]int),
		symbols: [len(verbs)]lipgloss.Style{
			VerbError:   bold("1"),
			VerbFail:    bold("1"),
			VerbWarn:    bold("3"),
			VerbInfo:    bold("7"),
			VerbSuccess: bold("2"),
			VerbWait:    bold("7"),
			VerbReady:   bold("2"),
			VerbStep:    dim,
			VerbDebug:   bold("8"),
			VerbTrace:   bold("5"),
		},
		dim: dim,
	}
}

// Render returns the event's lines, newline-terminated. clock puts the dim
// local HH:MM:SS of the event first.
func (h *Human) Render(e *Event, clock bool) string {
	pad := strings.Repeat("  ", h.place(e))
	stackPad := pad + "    "
	var b strings.Builder
	if clock {
		t, err := time.Parse(TimeLayout, e.Time)
		if err != nil {
			panic("stream: event time " + strconv.Quote(e.Time) + " is not " + TimeLayout)
		}
		b.WriteString(h.dim.Render(t.Local().Format("15:04:05")) + " ")
		stackPad = strings.Repeat(" ", len("15:04:05 ")) + stackPad
	}
	b.WriteString(pad)
	if e.Verb == VerbStep {
		b.WriteString("  ")
	}
	b.WriteString(h.symbols[e.Verb].Render(verbs[e.Verb].symbol) + " ")
	if len(e.Scope) > 0 {
		b.WriteString(h.dim.Render("["+strings.Join(e.Scope, " ")+"]") + " ")
	}
	b.WriteString(e.Msg)
	if e.Phase == PhaseEnd {
		b.WriteString(" " + h.dim.Render(humanDuration(e.DurMs)))
	}
	var stacks []string
	for _, f := range e.Fields {
		text, stack := fieldText(f.Value)
		b.WriteString(" " + h.dim.Render(f.Key+"="+text))
		if stack != "" {
			stacks = append(stacks, stack)
		}
	}
	b.WriteByte('\n')
	for _, stack := range stacks {
		for _, line := range strings.Split(strings.TrimRight(stack, "\n"), "\n") {
			b.WriteString(stackPad + h.dim.Render(line) + "\n")
		}
	}
	return b.String()
}

// place returns the event's depth and keeps the open spans' depths: a span
// sits one under its parent, a line one under its span, and an end at its
// start's depth. Unknown ids (a span never shown here, a remote parent) sit
// at the top.
func (h *Human) place(e *Event) int {
	under := func(id string) int {
		if d, ok := h.depth[id]; ok {
			return d + 1
		}
		return 0
	}
	switch e.Phase {
	case PhaseStart:
		d := under(e.ParentID)
		h.depth[e.SpanID] = d
		return d
	case PhaseEnd:
		d, ok := h.depth[e.SpanID]
		if !ok {
			d = under(e.ParentID)
		}
		delete(h.depth, e.SpanID)
		return d
	}
	if e.SpanID == "" {
		return 0
	}
	return under(e.SpanID)
}

// humanDuration: 0.012ms, 3.4ms, 254ms, 1.25s, then minutes.
func humanDuration(ms float64) string {
	switch {
	case ms < 1:
		return strconv.FormatFloat(ms, 'f', 3, 64) + "ms"
	case ms < 10:
		return strconv.FormatFloat(ms, 'f', 1, 64) + "ms"
	case ms < 1000:
		return strconv.FormatFloat(ms, 'f', 0, 64) + "ms"
	case ms < 60_000:
		return strconv.FormatFloat(ms/1000, 'f', 2, 64) + "s"
	}
	return (time.Duration(ms * float64(time.Millisecond))).Round(time.Second).String()
}

// GrepLine renders the event as the one line a pipe gets:
//
//	2026-10-12T12:20:39.123Z WARN  [payments] retrying attempt=2
//
// the time as written, the level word upper-case padded to five, the scope in
// brackets when present, the message, the fields as key=value, and an end
// event's dur_ms last. Newlines in the message are escaped so an event stays
// one line.
func (e *Event) GrepLine() string {
	level := strings.ToUpper(e.Verb.Level().String())
	var b strings.Builder
	b.WriteString(e.Time + " " + level + strings.Repeat(" ", 5-len(level)))
	if len(e.Scope) > 0 {
		b.WriteString(" [" + strings.Join(e.Scope, " ") + "]")
	}
	b.WriteString(" " + oneLine(e.Msg))
	for _, f := range e.Fields {
		text, _ := fieldText(f.Value)
		b.WriteString(" " + f.Key + "=" + text)
	}
	if e.Phase == PhaseEnd {
		b.WriteString(" dur_ms=" + string(AppendNumber(nil, e.DurMs, 64)))
	}
	return b.String()
}

func oneLine(s string) string {
	return strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(s)
}

// fieldText is how a field value reads after `key=`: a string as its text,
// quoted when it holds a space, a quote, an `=` or anything unprintable; an
// error as its message, the same way, with its stack returned apart; anything
// else as its JSON.
func fieldText(raw json.RawMessage) (text, stack string) {
	switch raw[0] {
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			panic("stream: field value " + string(raw) + " is not a JSON string: " + err.Error())
		}
		return quoteIfNeeded(s), ""
	case '{':
		if message, stack, ok := errorOf(raw); ok {
			return quoteIfNeeded(message), stack
		}
	}
	return string(raw), ""
}

// errorOf recognizes an error value by its shape: an object whose first keys
// are type then message, the order no other value is written in.
func errorOf(raw json.RawMessage) (message, stack string, ok bool) {
	entries, ok := ParseObject(raw)
	if !ok || len(entries) < 2 || entries[0].Key != "type" || entries[1].Key != "message" {
		return "", "", false
	}
	var e struct {
		Message string `json:"message"`
		Stack   string `json:"stack"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return "", "", false
	}
	return e.Message, e.Stack, true
}

func quoteIfNeeded(s string) string {
	if s == "" {
		return `""`
	}
	for _, r := range s {
		if r == ' ' || r == '"' || r == '=' || !unicode.IsPrint(r) {
			return strconv.Quote(s)
		}
	}
	return s
}
