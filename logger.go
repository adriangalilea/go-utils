package utils

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

type logLevel int

const (
	logSilent logLevel = iota
	logError
	logWarn
	logInfo
	logDebug
	logTrace
)

type logOps struct {
	mu       sync.Mutex
	warnOnce map[string]struct{}
	context  string
}

// Log filters by LOG_LEVEL (via KEV, so .env works) and prints through
// Format. Everything goes to stderr - stdout is reserved for data, so
// piping a tool's output never chokes on a log line.
var Log = &logOps{
	warnOnce: make(map[string]struct{}),
}

// NewLogger scopes a logger to {context}_LOG_LEVEL, falling back to LOG_LEVEL.
func NewLogger(context string) *logOps {
	return &logOps{
		warnOnce: make(map[string]struct{}),
		context:  context,
	}
}

// Both lookups pass a default so KEV caches misses - without it every
// suppressed log line would rescan the .env files on disk.
func (l *logOps) getLevel() logLevel {
	var level string

	if l.context != "" {
		level = KEV.Get(l.context+"_LOG_LEVEL", "")
	}

	if level == "" {
		level = KEV.Get("LOG_LEVEL", "info")
	}

	return l.parseLevel(strings.ToLower(level))
}

// An unknown level is a misconfiguration - scream instead of silently
// logging at info.
func (l *logOps) parseLevel(level string) logLevel {
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

func (l *logOps) shouldLog(msgLevel logLevel) bool {
	return msgLevel <= l.getLevel()
}

func (l *logOps) Error(args ...any) {
	if l.shouldLog(logError) {
		fmt.Fprintln(os.Stderr, Format.Error(args...))
	}
}

func (l *logOps) Warn(args ...any) {
	if l.shouldLog(logWarn) {
		fmt.Fprintln(os.Stderr, Format.Warn(args...))
	}
}

// A suppressed warning doesn't count as seen - it still fires if the
// log level allows warnings later.
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
	l.warnOnce[key] = struct{}{}
	l.mu.Unlock()

	fmt.Fprintln(os.Stderr, Format.Warn(args...))
}

func (l *logOps) Info(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Fprintln(os.Stderr, Format.Info(args...))
	}
}

func (l *logOps) Event(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Fprintln(os.Stderr, Format.Event(args...))
	}
}

func (l *logOps) Wait(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Fprintln(os.Stderr, Format.Wait(args...))
	}
}

func (l *logOps) Ready(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Fprintln(os.Stderr, Format.Ready(args...))
	}
}

func (l *logOps) Debug(args ...any) {
	if l.shouldLog(logDebug) {
		fmt.Fprintln(os.Stderr, Format.Debug(args...))
	}
}

func (l *logOps) Trace(args ...any) {
	if l.shouldLog(logTrace) {
		fmt.Fprintln(os.Stderr, Format.Trace(args...))
	}
}

// Indented sub-step line, no symbol.
func (l *logOps) Bootstrap(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Fprintln(os.Stderr, "   "+String(args...))
	}
}
