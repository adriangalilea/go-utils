package utils

import (
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/adriangalilea/go-utils/internal/stream"
)

// Log is the root logger. It writes the event stream of the logging contract
// shared with ts-utils and py-utils (github.com/adriangalilea/utils): what Go,
// TypeScript and Python programs write can be read, merged and traced
// together, byte for byte.
//
// Every verb takes a message, then at most one F of fields. The message says
// what happened, the fields say with what:
//
//	Log.Info("ready")
//	Log.Warn("retrying", F{"attempt": 2})
//	Log.Error("charge failed", F{"err": err})
//
// Verbs say what happened and each prints at a fixed level: Error and Fail
// (error), Warn and WarnOnce (warn), Info, Success, Wait, Ready and Step
// (info), Debug, Trace.
//
// Scope is the subsystem: Log.Scope("api").Scope("auth") writes
// scope ["api","auth"] and filters by API_AUTH_LOG_LEVEL, then API_LOG_LEVEL,
// then LOG_LEVEL, then SetLogLevel, then info. Levels are
// silent < error < warn < info < debug < trace, read live through KEV, so
// .env files work and one subsystem opens or goes quiet without a code change.
//
// A span is a piece of work and a span is a logger: what is written through
// it belongs to it. It has every verb, Span (a child), Set (fields learned
// while it runs, carried on its end) and Traceparent (its W3C header value to
// hand on). It ends with the function that opened it, failed when the
// function returns an error:
//
//	func charge(amount int) (err error) {
//	    s := Log.Span("charge", F{"amount": amount})
//	    defer s.End(&err)
//	    s.Warn("retrying", F{"attempt": 2})
//	    ...
//	}
//
// There is no context.Context: a function that logs inside a span takes the
// span. Log.Join(r.Header.Get("traceparent")) continues a remote trace; a
// process started with TRACEPARENT set continues it for its root spans.
//
// Output goes to stderr, stdout stays data. LOG_FORMAT=json writes one JSON
// event per line in the contract's key order; LOG_FORMAT=human writes symbols,
// colour, dim key=value fields and indented spans. The default is human when
// stderr is a terminal, json otherwise. LOG_TIME=1 opens human lines with a
// dim local HH:MM:SS. LOG_SERVICE names the program in every event (default:
// the executable's name). Each variable has a setter for code (SetLogFormat,
// SetLogTime, SetLogLevel, SetLogService), and the environment, read live,
// wins over it. Values match in any case; an unknown one panics. `logs`
// (cmd/logs) reads the JSON back as human or as grep lines.
var Log = &Logger{}

// F is the fields of an event. Keys are written sorted; values follow the
// contract's table (errors as objects with their Unwrap chain, time.Time as
// the event-time string, time.Duration as milliseconds, structs through
// encoding/json).
type F map[string]any

// Logger writes events in a scope, and inside a span when it is one's.
type Logger struct {
	scope     []string
	levelVars []string    // the scope's LOG_LEVEL variables, most specific first
	span      *Span       // the span what is written here belongs to
	remote    *remoteSpan // the span root spans continue; nil: TRACEPARENT's
}

type remoteSpan struct{ trace, span string }

// Scope returns a child logger whose events carry name after the parent's
// scope and whose level reads {SCOPE_PATH}_LOG_LEVEL first.
func (l *Logger) Scope(name string) *Logger {
	Assert(name != "", "a scope needs a name")
	scope := append(slices.Clone(l.scope), name)
	return &Logger{
		scope:     scope,
		levelVars: append([]string{envKey(scope) + "_LOG_LEVEL"}, l.levelVars...),
		span:      l.span,
		remote:    l.remote,
	}
}

// envKey: ["api", "auth-v2"] -> "API_AUTH_V2".
func envKey(scope []string) string {
	var b strings.Builder
	for i, segment := range scope {
		if i > 0 {
			b.WriteByte('_')
		}
		for _, r := range strings.ToUpper(segment) {
			if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
				b.WriteRune(r)
			} else {
				b.WriteByte('_')
			}
		}
	}
	return b.String()
}

// Join returns a logger whose root spans are children of the remote span
// named by a W3C traceparent value, as an HTTP traceparent header carries it.
// An absent value ("") returns l; a malformed one is input from outside, so it
// is ignored with a warning.
func (l *Logger) Join(traceparent string) *Logger {
	if traceparent == "" {
		return l
	}
	r, ok := parseTraceparent(traceparent)
	if !ok {
		l.warnOnce("\x02"+traceparent, "malformed traceparent ignored", F{"traceparent": traceparent})
		return l
	}
	return &Logger{scope: l.scope, levelVars: l.levelVars, remote: r}
}

func (l *Logger) Error(msg string, fields ...F)   { l.line(stream.VerbError, msg, fields) }
func (l *Logger) Fail(msg string, fields ...F)    { l.line(stream.VerbFail, msg, fields) }
func (l *Logger) Warn(msg string, fields ...F)    { l.line(stream.VerbWarn, msg, fields) }
func (l *Logger) Info(msg string, fields ...F)    { l.line(stream.VerbInfo, msg, fields) }
func (l *Logger) Success(msg string, fields ...F) { l.line(stream.VerbSuccess, msg, fields) }
func (l *Logger) Wait(msg string, fields ...F)    { l.line(stream.VerbWait, msg, fields) }
func (l *Logger) Ready(msg string, fields ...F)   { l.line(stream.VerbReady, msg, fields) }
func (l *Logger) Step(msg string, fields ...F)    { l.line(stream.VerbStep, msg, fields) }
func (l *Logger) Debug(msg string, fields ...F)   { l.line(stream.VerbDebug, msg, fields) }
func (l *Logger) Trace(msg string, fields ...F)   { l.line(stream.VerbTrace, msg, fields) }

// WarnOnce warns once per message and scope for the life of the process.
func (l *Logger) WarnOnce(msg string, fields ...F) {
	l.warnOnce(strings.Join(l.scope, "\x00")+"\x01"+msg, msg, single(fields))
}

// warnOnce warns the first time key is met. A warning filtered out does not
// count as seen. The memory is bounded: past 1024 distinct keys it forgets
// them all, and an old warning firing once more is the honest failure.
func (l *Logger) warnOnce(key, msg string, f F) {
	if !l.enabled(stream.LevelWarn) {
		return
	}
	warned.Lock()
	_, seen := warned.set[key]
	if !seen {
		if len(warned.set) >= 1024 {
			clear(warned.set)
		}
		warned.set[key] = struct{}{}
	}
	warned.Unlock()
	if !seen {
		l.write(stream.VerbWarn, msg, f)
	}
}

var warned = struct {
	sync.Mutex
	set map[string]struct{}
}{set: make(map[string]struct{})}

func single(fields []F) F {
	switch len(fields) {
	case 0:
		return nil
	case 1:
		return fields[0]
	}
	panic(&Panic{Message: String("a log call takes one F of fields, got", len(fields))})
}

func (l *Logger) line(v stream.Verb, msg string, fields []F) {
	f := single(fields)
	if l.enabled(v.Level()) {
		l.write(v, msg, f)
	}
}

// write emits a line. Inside a span it carries the trace and the nearest
// span that was shown.
func (l *Logger) write(v stream.Verb, msg string, f F) {
	e := l.event(v, msg)
	if s := l.span; s != nil {
		e.TraceID = s.trace
		e.SpanID = s.anchor
	}
	e.Fields = encodeFields(f)
	emit(&e)
}

func (l *Logger) event(v stream.Verb, msg string) stream.Event {
	return stream.Event{Time: stream.FormatTime(time.Now()), Verb: v, Msg: msg, Service: service(), Scope: l.scope}
}

func (l *Logger) enabled(level stream.Level) bool {
	return level <= l.threshold()
}

// threshold: the most specific scope variable set, then LOG_LEVEL, then
// SetLogLevel, then info. KEV caches misses, so a filtered line costs map
// lookups, not a scan of .env files.
func (l *Logger) threshold() stream.Level {
	for _, name := range l.levelVars {
		if v := KEV.get(name, false, ""); v != "" {
			return parseLevel(name, v)
		}
	}
	if v := KEV.get("LOG_LEVEL", false, ""); v != "" {
		return parseLevel("LOG_LEVEL", v)
	}
	settings.Lock()
	defer settings.Unlock()
	if settings.levelSet {
		return settings.level
	}
	return stream.LevelInfo
}

func parseLevel(name, value string) stream.Level {
	level, ok := stream.ParseLevel(strings.ToLower(value))
	Assert(ok, "unknown", name+":", value, "(want silent, error, warn, info, debug, trace)")
	return level
}

// Span starts a piece of work under this logger: a child of its span, else a
// root span (of the joined remote span, else of TRACEPARENT's, else a new
// trace). Whether it is shown is decided now, once, by its start's level (info)
// against the threshold: a shown span writes its start now and its end at
// End; a hidden one writes neither, and what it holds belongs to the nearest
// span that was shown.
func (l *Logger) Span(name string, fields ...F) *Span {
	f := single(fields)
	trace, parent := l.parentage()
	s := &Span{name: name, trace: trace, id: newID(8), parent: parent, anchor: parent, start: time.Now(), fields: F{}}
	maps.Copy(s.fields, f)
	s.Logger = &Logger{scope: l.scope, levelVars: l.levelVars, span: s}
	s.shown = s.enabled(stream.VerbWait.Level())
	if !s.shown {
		return s
	}
	s.anchor = s.id
	e := s.own(stream.VerbWait, stream.PhaseStart)
	e.Fields = encodeFields(f)
	emit(&e)
	return s
}

func (l *Logger) parentage() (trace, parent string) {
	if l.span != nil {
		return l.span.trace, l.span.anchor
	}
	r := l.remote
	if r == nil {
		r = envTraceparent()
	}
	if r != nil {
		return r.trace, r.span
	}
	return newID(16), ""
}

// Span is a piece of work: a start event, the lines and spans written through
// it, an end event. It embeds the Logger its lines go through, so every verb,
// Scope and Span work on it.
type Span struct {
	*Logger
	name   string
	trace  string
	id     string
	parent string // the shown span this one hangs from; "" for a root
	shown  bool   // its start was written, so its end will be
	anchor string // the shown span what this one holds belongs to: id when shown, else parent
	start  time.Time

	mu     sync.Mutex
	fields F
	ended  bool
}

// Set adds fields learned while the span runs; its end event carries them
// with the ones it started with.
func (s *Span) Set(f F) {
	s.mu.Lock()
	defer s.mu.Unlock()
	Assert(!s.ended, "Set on ended span", s.name)
	maps.Copy(s.fields, f)
}

// Traceparent is the W3C traceparent value naming this span, to hand to
// another process in an HTTP traceparent header or a TRACEPARENT variable.
// A hidden span names the nearest shown one, and its own id only when no
// ancestor is shown.
func (s *Span) Traceparent() string {
	id := s.anchor
	if id == "" {
		id = s.id
	}
	return "00-" + s.trace + "-" + id + "-01"
}

// End writes the span's end when its start was written. err nil or pointing
// to nil: ok, verb success. Pointing to an error: verb fail, level error, the
// error in fields.err. Call it once, deferred with the function's named
// error: defer s.End(&err).
func (s *Span) End(err *error) {
	s.mu.Lock()
	ended := s.ended
	s.ended = true
	s.mu.Unlock()
	Assert(!ended, "span ended twice:", s.name)
	dur := time.Since(s.start)
	if !s.shown {
		return
	}
	verb := stream.VerbSuccess
	if err != nil && *err != nil {
		verb = stream.VerbFail
		s.fields["err"] = *err
	}
	e := s.own(verb, stream.PhaseEnd)
	e.DurMs = math.Round(float64(dur.Nanoseconds())/1e3) / 1e3
	e.Fields = encodeFields(s.fields)
	emit(&e)
}

func (s *Span) own(v stream.Verb, phase stream.Phase) stream.Event {
	e := s.event(v, s.name)
	e.Phase = phase
	e.TraceID = s.trace
	e.SpanID = s.id
	e.ParentID = s.parent
	return e
}

// newID is n random bytes as lowercase hex, never all zeros (W3C forbids it).
func newID(n int) string {
	b := make([]byte, n)
	for {
		_, _ = rand.Read(b)
		if slices.ContainsFunc(b, func(x byte) bool { return x != 0 }) {
			return hex.EncodeToString(b)
		}
	}
}

// parseTraceparent reads a W3C traceparent: version-trace_id-parent_id-flags,
// lowercase hex, ids not all zeros. Version 00 is exactly 55 characters; a
// later version may append fields after a dash.
func parseTraceparent(v string) (*remoteSpan, bool) {
	if len(v) < 55 || v[2] != '-' || v[35] != '-' || v[52] != '-' {
		return nil, false
	}
	version, trace, span, flags := v[:2], v[3:35], v[36:52], v[53:55]
	if !lowerHex(version) || !lowerHex(trace) || !lowerHex(span) || !lowerHex(flags) || version == "ff" {
		return nil, false
	}
	if (version == "00" && len(v) != 55) || (len(v) > 55 && v[55] != '-') {
		return nil, false
	}
	if strings.Trim(trace, "0") == "" || strings.Trim(span, "0") == "" {
		return nil, false
	}
	return &remoteSpan{trace: trace, span: span}, true
}

func lowerHex(s string) bool {
	return strings.Trim(s, "0123456789abcdef") == ""
}

// TRACEPARENT is read once, at the first root span: it is how a process was
// started, not something that changes.
var (
	traceparentOnce sync.Once
	traceparentEnv  *remoteSpan
)

func envTraceparent() *remoteSpan {
	traceparentOnce.Do(func() {
		v := os.Getenv("TRACEPARENT")
		if v == "" {
			return
		}
		r, ok := parseTraceparent(v)
		if !ok {
			Log.Warn("malformed TRACEPARENT ignored", F{"traceparent": v})
			return
		}
		traceparentEnv = r
	})
	return traceparentEnv
}

// settings are the program's own defaults. For every one of them the
// environment wins: the operator's variable overrides what code chose.
var settings struct {
	sync.Mutex
	level    stream.Level
	levelSet bool
	format   string
	clock    string
	service  string
}

// SetLogLevel is the program's default threshold: silent, error, warn, info,
// debug or trace, in any case. LOG_LEVEL and the scope variables override it.
func SetLogLevel(level string) {
	parsed := parseLevel("SetLogLevel", level)
	settings.Lock()
	defer settings.Unlock()
	settings.level, settings.levelSet = parsed, true
}

// SetLogFormat chooses "human" or "json" when LOG_FORMAT is unset.
func SetLogFormat(format string) {
	parsed := parseFormat("SetLogFormat", format)
	settings.Lock()
	defer settings.Unlock()
	settings.format = parsed
}

// SetLogTime puts the local clock on human lines when LOG_TIME is unset.
func SetLogTime(on bool) {
	settings.Lock()
	defer settings.Unlock()
	settings.clock = "0"
	if on {
		settings.clock = "1"
	}
}

// SetLogService names the program in every event when LOG_SERVICE is unset.
func SetLogService(name string) {
	Assert(name != "", "a log service needs a name")
	settings.Lock()
	defer settings.Unlock()
	settings.service = name
}

// The environment is read live through KEV, like the levels: an operator's
// variable (or a .env entry) overrides the setter, and KEV caches misses, so
// an unset variable costs a map lookup per event.

func service() string {
	if name := KEV.get("LOG_SERVICE", false, ""); name != "" {
		return name
	}
	settings.Lock()
	defer settings.Unlock()
	if settings.service != "" {
		return settings.service
	}
	name := filepath.Base(os.Args[0])
	name = strings.TrimSuffix(name, filepath.Ext(name))
	Assert(name != "" && name != "." && name != string(filepath.Separator), "no executable name to call the service, set LOG_SERVICE")
	return name
}

// output resolves the format and the clock: the environment, then the
// setter, then the default (human when stderr is a terminal, json elsewhere;
// no clock).
func output() (human, clock bool) {
	format := KEV.get("LOG_FORMAT", false, "")
	if format != "" {
		format = parseFormat("LOG_FORMAT", format)
	}
	logTime := KEV.get("LOG_TIME", false, "")
	Assert(logTime == "" || logTime == "1" || logTime == "0", "unknown LOG_TIME:", logTime, "(want 1, 0)")
	settings.Lock()
	defer settings.Unlock()
	format = cmp.Or(format, settings.format)
	return format == "human" || (format == "" && stderrTTY()), cmp.Or(logTime, settings.clock) == "1"
}

func parseFormat(name, value string) string {
	format := strings.ToLower(value)
	Assert(format == "human" || format == "json", "unknown", name+":", value, "(want human, json)")
	return format
}

// stderrTTY is decided once: stderr does not change class mid-run.
var stderrTTY = sync.OnceValue(func() bool {
	stat, err := os.Stderr.Stat()
	return err == nil && stat.Mode()&os.ModeCharDevice != 0
})

// out is where events go. One write per event under the lock, so lines from
// concurrent goroutines never interleave; the human renderer keeps the depth
// of open spans across events.
var out = struct {
	sync.Mutex
	w     io.Writer
	buf   []byte
	human *stream.Human
}{w: os.Stderr}

// emit writes the event. A failed write to stderr is dropped: there is
// nowhere left to report it.
func emit(e *stream.Event) {
	human, clock := output()
	out.Lock()
	defer out.Unlock()
	if !human {
		out.buf = append(e.AppendJSON(out.buf[:0]), '\n')
		_, _ = out.w.Write(out.buf)
		return
	}
	if out.human == nil {
		out.human = stream.NewHuman(out.w)
	}
	_, _ = io.WriteString(out.w, out.human.Render(e, clock))
}
