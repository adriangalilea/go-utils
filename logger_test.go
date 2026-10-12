package utils

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/adriangalilea/go-utils/internal/stream"
)

// The variables the logger reads, blanked in KEV memory so the shell running
// the tests cannot change what they see (KEV reads memory before the OS).
var logVars = []string{"LOG_FORMAT", "LOG_TIME", "LOG_SERVICE", "LOG_LEVEL",
	"API_LOG_LEVEL", "API_AUTH_LOG_LEVEL", "DB_LOG_LEVEL", "DB_FAST_LOG_LEVEL"}

// capture runs fn with the logger writing to a buffer in format, the
// environment blank and the settings at their defaults, and returns what it
// wrote.
func capture(t *testing.T, format string, fn func()) string {
	t.Helper()
	for _, name := range logVars {
		KEV.Set(name, "")
	}
	var buf bytes.Buffer
	out.Lock()
	out.w, out.human = &buf, nil
	out.Unlock()
	settings.Lock()
	settings.levelSet, settings.format, settings.clock, settings.service = false, format, "", "svc"
	settings.Unlock()
	t.Cleanup(func() {
		out.Lock()
		out.w, out.human = os.Stderr, nil
		out.Unlock()
		settings.Lock()
		settings.levelSet, settings.format, settings.clock, settings.service = false, "", "", ""
		settings.Unlock()
		for _, name := range logVars {
			KEV.Unset(name)
		}
	})
	fn()
	return buf.String()
}

// events parses every line written as an event, failing on any that is not.
func events(t *testing.T, written string) []stream.Event {
	t.Helper()
	var es []stream.Event
	for _, line := range strings.Split(strings.TrimSuffix(written, "\n"), "\n") {
		e, ok := stream.Parse([]byte(line))
		if !ok {
			t.Fatalf("not an event in the contract's shape: %s", line)
		}
		es = append(es, e)
	}
	return es
}

var eventTime = regexp.MustCompile(`"time":"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z"`)

func TestJSONShapeAndKeyOrder(t *testing.T) {
	got := capture(t, "json", func() {
		Log.Info("daemon starting")
		Log.Scope("payments").Warn("retrying", F{"attempt": 2, "backoff": "1s"})
		Log.Scope("payments").WarnOnce("deprecated flag")
		Log.Scope("payments").WarnOnce("deprecated flag")
		Log.Info("empty fields", F{})
	})
	want := `{"time":T,"level":"info","verb":"info","msg":"daemon starting","service":"svc"}
{"time":T,"level":"warn","verb":"warn","msg":"retrying","service":"svc","scope":["payments"],"fields":{"attempt":2,"backoff":"1s"}}
{"time":T,"level":"warn","verb":"warn","msg":"deprecated flag","service":"svc","scope":["payments"]}
{"time":T,"level":"info","verb":"info","msg":"empty fields","service":"svc"}
`
	if got := eventTime.ReplaceAllString(got, `"time":T`); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestTwoFieldsPanics(t *testing.T) {
	defer func() {
		if _, ok := recover().(*Panic); !ok {
			t.Fatal("two F must panic with *Panic")
		}
	}()
	Log.Info("x", F{"a": 1}, F{"b": 2})
}

func value(v any) string { return string(appendValue(nil, v, nil)) }

func TestFieldValues(t *testing.T) {
	type point struct {
		Y int    `json:"y"`
		X string `json:"x"`
	}
	self := map[string]any{"name": "loop"}
	self["self"] = self
	cases := []struct {
		in   any
		want string
	}{
		{F{"z": 1, "a": F{"y": []any{1, "x"}, "b": nil}}, `{"a":{"b":null,"y":[1,"x"]},"z":1}`},
		{2.0, `2`},
		{1.5, `1.5`},
		{1e-7, `1e-7`},
		{1e21, `1e+21`},
		{math.Copysign(0, -1), `0`},
		{math.NaN(), `"NaN"`},
		{math.Inf(1), `"Infinity"`},
		{math.Inf(-1), `"-Infinity"`},
		{float32(0.1), `0.1`},
		{"a \"quoted\"\n<tag>", `"a \"quoted\"\n<tag>"`},
		{[]byte("hi"), `"aGk="`},
		{1500 * time.Microsecond, `1.5`},
		{time.Date(2026, 10, 12, 14, 20, 39, 123456789, time.FixedZone("CEST", 2*3600)), `"2026-10-12T12:20:39.123Z"`},
		{new(big.Int).Lsh(big.NewInt(1), 70), `"1180591620717411303424"`},
		{point{Y: 1, X: "b"}, `{"x":"b","y":1}`},
		{&point{Y: 2, X: "c"}, `{"x":"c","y":2}`},
		{map[int]string{2: "b", 1: "a"}, `{"1":"a","2":"b"}`},
		{[2]int{1, 2}, `[1,2]`},
		{self, `{"name":"loop","self":"[circular]"}`},
		{complex(1, 2), `"(1+2i)"`},
		{[]int(nil), `null`},
	}
	for _, c := range cases {
		if got := value(c.in); got != c.want {
			t.Errorf("%#v\n got %s\nwant %s", c.in, got, c.want)
		}
	}
}

func TestErrorSerialization(t *testing.T) {
	sourced := &SourcedError{
		Source: "stripe", Operation: "charge", Message: "card declined", Status: 402,
		Cause: errors.New("http 402"), Context: map[string]any{"z": 1, "customer": "c1"},
	}
	wrapped := fmt.Errorf("charge: %w", sourced)
	want := `{"type":"*fmt.wrapError","message":"charge: [stripe:charge status=402] card declined",` +
		`"cause":{"type":"*utils.SourcedError","message":"card declined",` +
		`"cause":{"type":"*errors.errorString","message":"http 402"},` +
		`"source":"stripe","operation":"charge","status":402,"context":{"customer":"c1","z":1}}}`
	if got := value(wrapped); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	bare := &SourcedError{Source: "db", Operation: "ping", Message: "refused"}
	if got := value(bare); got != `{"type":"*utils.SourcedError","message":"refused","source":"db","operation":"ping"}` {
		t.Fatalf("status and context appear only with a value: %s", got)
	}
}

func TestSpanNestingAndIDs(t *testing.T) {
	es := events(t, capture(t, "json", func() {
		s := Log.Span("sync", F{"repo": "ui"})
		s.Info("cloned", F{"files": 3})
		c := s.Span("build")
		c.Warn("slow step")
		c.End(nil)
		s.Set(F{"pushed": true})
		var none error
		s.End(&none)
	}))
	if len(es) != 6 {
		t.Fatalf("want 6 events, got %d", len(es))
	}
	start, line, childStart, childLine, childEnd, end := es[0], es[1], es[2], es[3], es[4], es[5]
	trace := start.TraceID
	for _, e := range es {
		if e.TraceID != trace {
			t.Fatalf("one trace: %s vs %s", e.TraceID, trace)
		}
	}
	if start.Phase != stream.PhaseStart || start.Verb != stream.VerbWait || start.ParentID != "" {
		t.Fatalf("root start: %+v", start)
	}
	if line.SpanID != start.SpanID || line.ParentID != "" || line.Phase != stream.PhaseNone {
		t.Fatalf("a line names its span: %+v", line)
	}
	if childStart.ParentID != start.SpanID || childStart.SpanID == start.SpanID {
		t.Fatalf("child names its parent: %+v", childStart)
	}
	if childLine.SpanID != childStart.SpanID || childEnd.SpanID != childStart.SpanID {
		t.Fatalf("child events share its id")
	}
	if end.Phase != stream.PhaseEnd || end.Verb != stream.VerbSuccess || end.SpanID != start.SpanID || end.DurMs < 0 {
		t.Fatalf("root end: %+v", end)
	}
	if got := fieldsJSON(end); got != `{"pushed":true,"repo":"ui"}` {
		t.Fatalf("end carries start fields and Set: %s", got)
	}
}

func fieldsJSON(e stream.Event) string {
	var b []byte
	b = append(b, '{')
	for i, f := range e.Fields {
		if i > 0 {
			b = append(b, ',')
		}
		b = stream.AppendString(b, f.Key)
		b = append(b, ':')
		b = append(b, f.Value...)
	}
	return string(append(b, '}'))
}

func deploy() (err error) {
	s := Log.Span("deploy")
	defer s.End(&err)
	return errors.New("boom")
}

func TestFailedSpan(t *testing.T) {
	es := events(t, capture(t, "json", func() { _ = deploy() }))
	end := es[1]
	if end.Verb != stream.VerbFail || end.Verb.Level() != stream.LevelError {
		t.Fatalf("failed end: %+v", end)
	}
	if got := fieldsJSON(end); got != `{"err":{"type":"*errors.errorString","message":"boom"}}` {
		t.Fatalf("error in fields.err: %s", got)
	}
	defer func() {
		if _, ok := recover().(*Panic); !ok {
			t.Fatal("End twice must panic with *Panic")
		}
	}()
	s := Log.Span("twice")
	s.End(nil)
	s.End(nil)
}

func TestHiddenSpanReparenting(t *testing.T) {
	var outer, hidden, lonely *Span
	es := events(t, capture(t, "json", func() {
		KEV.Set("DB_LOG_LEVEL", "warn")
		KEV.Set("DB_FAST_LOG_LEVEL", "info")
		outer = Log.Span("request")
		hidden = outer.Scope("db").Span("query")
		hidden.Warn("slow query")
		fetch := hidden.Scope("fast").Span("fetch")
		fetch.End(nil)
		failure := errors.New("timeout")
		hidden.End(&failure)
		outer.End(nil)
		lonely = Log.Scope("db").Span("lonely")
		lonely.Warn("alone")
		lonely.End(nil)
	}))
	verbs := make([]string, len(es))
	for i, e := range es {
		verbs[i] = e.Verb.String() + " " + e.Msg
	}
	if got := strings.Join(verbs, ", "); got != "wait request, warn slow query, wait fetch, success fetch, success request, warn alone" {
		t.Fatalf("a hidden span writes neither start nor end, failed or not: %s", got)
	}
	request, slow, fetchStart, alone := es[0], es[1], es[2], es[5]
	if slow.SpanID != request.SpanID {
		t.Fatalf("a line in a hidden span belongs to the nearest shown one: %+v", slow)
	}
	if fetchStart.ParentID != request.SpanID {
		t.Fatalf("a child of a hidden span names the nearest shown one: %+v", fetchStart)
	}
	if want := "00-" + request.TraceID + "-" + request.SpanID + "-01"; hidden.Traceparent() != want {
		t.Fatalf("hidden traceparent %s, want %s", hidden.Traceparent(), want)
	}
	if alone.SpanID != "" || alone.TraceID != "" {
		t.Fatalf("no shown ancestor: a line carries no ids, like one outside any span: %+v", alone)
	}
	if want := "00-" + lonely.trace + "-" + lonely.id + "-01"; lonely.Traceparent() != want {
		t.Fatalf("no shown ancestor: own id %s, want %s", lonely.Traceparent(), want)
	}
}

const remote = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

func TestJoin(t *testing.T) {
	if Log.Join("") != Log {
		t.Fatal("Join of an absent value returns the logger")
	}
	es := events(t, capture(t, "json", func() {
		joined := Log.Join(remote)
		joined.Info("outside any span")
		s := joined.Span("handle")
		s.End(nil)
		Log.Join("garbage")
		Log.Join("garbage")
		Log.Join("00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01")
	}))
	if len(es) != 5 {
		t.Fatalf("want 5 events, got %d", len(es))
	}
	if es[0].TraceID != "" || es[0].SpanID != "" {
		t.Fatalf("a joined logger's lines outside spans carry no ids: %+v", es[0])
	}
	if es[1].TraceID != "0af7651916cd43dd8448eb211c80319c" || es[1].ParentID != "b7ad6b7169203331" {
		t.Fatalf("root span of a joined logger continues the remote span: %+v", es[1])
	}
	if es[3].Verb != stream.VerbWarn || es[4].Verb != stream.VerbWarn || es[3].Fields[0].Key != "traceparent" {
		t.Fatalf("a malformed traceparent warns once per distinct value: %+v", es[3:])
	}
}

// TRACEPARENT is read at process start, so the check runs in a child process.
func TestTraceparentEnv(t *testing.T) {
	if os.Getenv("GO_UTILS_TRACEPARENT_CHILD") == "1" {
		Log.Info("outside")
		s := Log.Span("root")
		s.Span("child").End(nil)
		s.End(nil)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestTraceparentEnv$")
	cmd.Env = append(os.Environ(), "GO_UTILS_TRACEPARENT_CHILD=1", "TRACEPARENT="+remote, "LOG_FORMAT=json", "LOG_LEVEL=info")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("child: %v\n%s", err, stderr.String())
	}
	es := events(t, stderr.String())
	if len(es) != 5 || es[0].TraceID != "" {
		t.Fatalf("lines outside spans carry no ids: %s", stderr.String())
	}
	root, child := es[1], es[2]
	if root.TraceID != "0af7651916cd43dd8448eb211c80319c" || root.ParentID != "b7ad6b7169203331" {
		t.Fatalf("a root span joins TRACEPARENT: %+v", root)
	}
	if child.TraceID != root.TraceID || child.ParentID != root.SpanID {
		t.Fatalf("its child stays local: %+v", child)
	}
}

func TestLevelResolution(t *testing.T) {
	capture(t, "json", func() {
		api, auth := Log.Scope("api"), Log.Scope("api").Scope("auth")
		check := func(l *Logger, want stream.Level, why string) {
			t.Helper()
			if got := l.threshold(); got != want {
				t.Fatalf("%s: got %s, want %s", why, got, want)
			}
		}
		check(auth, stream.LevelInfo, "nothing set: info")
		SetLogLevel("debug")
		check(auth, stream.LevelDebug, "SetLogLevel is the program's default")
		KEV.Set("LOG_LEVEL", "WARN")
		check(auth, stream.LevelWarn, "LOG_LEVEL wins over code, in any case")
		KEV.Set("API_LOG_LEVEL", "trace")
		check(api, stream.LevelTrace, "the scope's variable wins over LOG_LEVEL")
		check(auth, stream.LevelTrace, "a nested scope reads its parent's variable")
		KEV.Set("API_AUTH_LOG_LEVEL", "silent")
		check(auth, stream.LevelSilent, "the most specific variable wins")
		check(api, stream.LevelTrace, "a child's variable does not reach its parent")
		if got := envKey([]string{"api", "auth-v2", "go check"}); got != "API_AUTH_V2_GO_CHECK" {
			t.Fatalf("envKey: %s", got)
		}
		KEV.Set("LOG_LEVEL", "verbose")
		defer func() {
			if _, ok := recover().(*Panic); !ok {
				t.Fatal("an unknown level must panic with *Panic")
			}
		}()
		Log.Info("x")
	})
}

var humanDuration = regexp.MustCompile(` \d+(\.\d+)?(ms|s)\b`)

func TestHumanSpan(t *testing.T) {
	got := capture(t, "human", func() {
		s := Log.Span("sync", F{"repo": "ui"})
		s.Info("cloned", F{"files": 3, "note": "two words"})
		c := s.Span("build")
		c.Step("compile")
		c.Warn("slow step", F{"ms": 1200})
		c.End(nil)
		s.End(nil)
		_ = deploy()
		Log.Scope("api").Error("charge failed", F{"err": errors.New("card declined")})
	})
	want := `○ sync repo=ui
    cloned files=3 note="two words"
  ○ build
      • compile
    ⚠ slow step ms=1200
  ✓ build D
✓ sync D repo=ui
○ deploy
⨯ deploy D err=boom
⨯ [api] charge failed err="card declined"
`
	if got := humanDuration.ReplaceAllString(got, " D"); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}
