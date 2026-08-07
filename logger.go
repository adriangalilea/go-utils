package utils

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Log is the terminal-first, file-honest logger.
//
// Doctrine:
//
// Two independent decisions, both automatic, both forceable from the
// environment:
//
//	LOG_FORMAT=human|record  how lines render. Default: human when stderr
//	                         is a TTY (symbols, color), record when it is
//	                         not (level words, plain text).
//	LOG_TIME=1|0             whether lines open with time. Default: on in
//	                         record, off in human.
//
// A file collecting evidence gets the full instant, 2026-08-07T12:34:56Z,
// UTC to match the ledger convention, because a triage line whose age is
// unknowable is worthless. A human who turns time on gets a dim local
// 14:32:56, because they know today's date. LOG_TIME=0 in record mode is
// for sinks that stamp lines themselves (journald, Cloudflare).
// NO_COLOR/FORCE_COLOR only affect color inside human rendering.
//
// The record line is identical across go-utils, ts-utils and py-utils by
// design, so logs from all three languages share one grep surface:
//
//	2026-08-07T12:34:56Z WARN  [theater] scan failed: EOF
//
// UTC RFC3339, level word padded to five columns, scope in brackets when
// present. Level words instead of symbols: `rg WARN` beats `rg ⚠`, and
// words survive viewers that mangle Unicode.
//
// Levels filter noise: silent < error < warn < info < debug < trace, read
// live from LOG_LEVEL (via KEV, so .env works). An unknown level panics.
// Verbs express outcome and are renderings, never levels: Success, Wait,
// Ready and Step render at info; everything filters by its level alone.
//
// Scope is the one composition primitive: Log.Scope("theater") returns a
// child that prints [theater] and reads THEATER_LOG_LEVEL before LOG_LEVEL,
// so one subsystem can be silenced or opened up from the environment
// without touching code. Scopes nest; the most specific level wins.
//
// Everything goes to stderr. stdout is reserved for data, so piping a
// tool's output never chokes on a log line.
type logLevel int

const (
	logSilent logLevel = iota
	logError
	logWarn
	logInfo
	logDebug
	logTrace
)

var levelWords = map[logLevel]string{
	logError: "ERROR",
	logWarn:  "WARN",
	logInfo:  "INFO",
	logDebug: "DEBUG",
	logTrace: "TRACE",
}

type logOps struct {
	mu       sync.Mutex
	warnOnce map[string]struct{}
	scope    []string
}

var Log = &logOps{warnOnce: make(map[string]struct{})}

// Scope returns a child logger that prints [name] on every line and
// resolves its level from {NAME}_LOG_LEVEL before LOG_LEVEL. Scopes nest:
// Log.Scope("api").Scope("auth") prints [api auth] and checks
// AUTH_LOG_LEVEL, then API_LOG_LEVEL, then LOG_LEVEL.
func (l *logOps) Scope(name string) *logOps {
	return &logOps{
		warnOnce: make(map[string]struct{}),
		scope:    append(append([]string{}, l.scope...), name),
	}
}

// Both lookups pass a default so KEV caches misses - without it every
// suppressed log line would rescan the .env files on disk.
func (l *logOps) getLevel() logLevel {
	for i := len(l.scope) - 1; i >= 0; i-- {
		if level := KEV.Get(envKey(l.scope[i])+"_LOG_LEVEL", ""); level != "" {
			return parseLevel(strings.ToLower(level))
		}
	}
	return parseLevel(strings.ToLower(KEV.Get("LOG_LEVEL", "info")))
}

func envKey(scope string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(scope) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// An unknown level is a misconfiguration - scream instead of silently
// logging at info.
func parseLevel(level string) logLevel {
	switch level {
	case "silent":
		return logSilent
	case "error":
		return logError
	case "warn", "warning":
		return logWarn
	case "info":
		return logInfo
	case "debug":
		return logDebug
	case "trace":
		return logTrace
	default:
		panic(&Panic{Message: String("unknown log level:", level, "(want silent/error/warn/info/debug/trace)")})
	}
}

var (
	modeOnce     sync.Once
	recordActive bool
	timeActive   bool
)

// Both decisions are made once per process: stderr does not change class
// mid-run, and deciding per line would put a KEV lookup on every call.
func resolveOutput() {
	switch format := KEV.Get("LOG_FORMAT", ""); format {
	case "":
		stat, err := os.Stderr.Stat()
		recordActive = err != nil || stat.Mode()&os.ModeCharDevice == 0
	case "human":
		recordActive = false
	case "record":
		recordActive = true
	default:
		panic(&Panic{Message: String("unknown LOG_FORMAT:", format, "(want human/record)")})
	}

	switch logTime := KEV.Get("LOG_TIME", ""); logTime {
	case "":
		timeActive = recordActive
	case "1", "true":
		timeActive = true
	case "0", "false":
		timeActive = false
	default:
		panic(&Panic{Message: String("unknown LOG_TIME:", logTime, "(want 1/0)")})
	}
}

func inRecordMode() bool {
	modeOnce.Do(resolveOutput)
	return recordActive
}

func timestampsOn() bool {
	modeOnce.Do(resolveOutput)
	return timeActive
}

// recordLine is pure so the format stays testable against the golden line
// the three sibling libraries share.
func recordLine(t time.Time, level, scope, message string) string {
	return t.UTC().Format(time.RFC3339) + " " + levelLine(level, scope, message)
}

func levelLine(level, scope, message string) string {
	line := level + strings.Repeat(" ", 5-len(level))
	if scope != "" {
		line += " [" + scope + "]"
	}
	return line + " " + message
}

func (l *logOps) shouldLog(msgLevel logLevel) bool {
	return msgLevel <= l.getLevel()
}

func (l *logOps) emit(msgLevel logLevel, msgType messageType, args ...any) {
	if !l.shouldLog(msgLevel) {
		return
	}

	if inRecordMode() {
		line := levelLine(levelWords[msgLevel], strings.Join(l.scope, " "), String(args...))
		if timestampsOn() {
			line = time.Now().UTC().Format(time.RFC3339) + " " + line
		}
		fmt.Fprintln(os.Stderr, line)
		return
	}

	if len(l.scope) > 0 {
		tag := "[" + strings.Join(l.scope, " ") + "]"
		if Format.colorEnabled {
			tag = grayStyle.Render(tag)
		}
		args = append([]any{tag}, args...)
	}
	fmt.Fprintln(os.Stderr, humanTimePrefix()+Format.formatMessage(msgType, args...))
}

// A human who opts into time gets the local clock, dim so the message
// stays the loudest thing on the line.
func humanTimePrefix() string {
	if !timestampsOn() {
		return ""
	}
	ts := time.Now().Format("15:04:05")
	if Format.colorEnabled {
		ts = grayStyle.Render(ts)
	}
	return ts + " "
}

func (l *logOps) Error(args ...any) {
	l.emit(logError, msgError, args...)
}

func (l *logOps) Warn(args ...any) {
	l.emit(logWarn, msgWarn, args...)
}

// A suppressed warning doesn't count as seen - it still fires if the log
// level allows warnings later. The set is capped so a daemon emitting
// unbounded distinct warnings can't leak; on overflow it purges, and an old
// warning firing once more is the honest failure mode.
func (l *logOps) WarnOnce(args ...any) {
	if !l.shouldLog(logWarn) {
		return
	}

	key := String(args...)

	l.mu.Lock()
	if _, exists := l.warnOnce[key]; exists {
		l.mu.Unlock()
		return
	}
	if len(l.warnOnce) >= 1024 {
		clear(l.warnOnce)
	}
	l.warnOnce[key] = struct{}{}
	l.mu.Unlock()

	l.emit(logWarn, msgWarn, args...)
}

func (l *logOps) Info(args ...any) {
	l.emit(logInfo, msgInfo, args...)
}

func (l *logOps) Success(args ...any) {
	l.emit(logInfo, msgSuccess, args...)
}

func (l *logOps) Wait(args ...any) {
	l.emit(logInfo, msgWait, args...)
}

func (l *logOps) Ready(args ...any) {
	l.emit(logInfo, msgReady, args...)
}

// Step is the indented sub-line inside a startup or task sequence.
func (l *logOps) Step(args ...any) {
	if !l.shouldLog(logInfo) {
		return
	}

	if inRecordMode() {
		l.emit(logInfo, msgStep, args...)
		return
	}
	fmt.Fprintln(os.Stderr, humanTimePrefix()+"  "+Format.formatMessage(msgStep, args...))
}

func (l *logOps) Debug(args ...any) {
	l.emit(logDebug, msgDebug, args...)
}

func (l *logOps) Trace(args ...any) {
	l.emit(logTrace, msgTrace, args...)
}
