package utils

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// logLevel for filtering output
type logLevel int

const (
	logSilent logLevel = iota
	logError
	logWarn
	logInfo
	logDebug
	logTrace
)

// logOps handles all logging operations with level filtering
type logOps struct {
	mu       sync.Mutex
	warnOnce map[string]struct{}
	context  string // Optional context for context-specific log levels
}

// Log provides logging operations with level filtering
var Log = &logOps{
	warnOnce: make(map[string]struct{}),
}

// NewLogger creates a logger for a specific context
// It will check {context}_LOG_LEVEL first, then fall back to LOG_LEVEL
func NewLogger(context string) *logOps {
	return &logOps{
		warnOnce: make(map[string]struct{}),
		context:  context,
	}
}

// getLevel returns the current log level from KEV.
// If logger has a context, checks {context}_LOG_LEVEL first.
// Both lookups pass a default so KEV caches misses - without it every
// suppressed log line would rescan the .env files on disk.
func (l *logOps) getLevel() logLevel {
	var level string

	if l.context != "" {
		level = KEV.Get(l.context+"_LOG_LEVEL", "")
	}

	// If no context or context level not set, use general LOG_LEVEL
	if level == "" {
		level = KEV.Get("LOG_LEVEL", "info")
	}

	return l.parseLevel(strings.ToLower(level))
}

// parseLevel converts a string to logLevel. An unknown level is a
// misconfiguration - scream instead of silently logging at info.
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

// shouldLog checks if message should be logged at given level
func (l *logOps) shouldLog(msgLevel logLevel) bool {
	currentLevel := l.getLevel()
	return msgLevel <= currentLevel
}

// Error logs an error message to stderr (always logs unless silent)
func (l *logOps) Error(args ...any) {
	if l.shouldLog(logError) {
		fmt.Fprintln(os.Stderr, Format.Error(args...))
	}
}

// Warn logs a warning message if level allows
func (l *logOps) Warn(args ...any) {
	if l.shouldLog(logWarn) {
		fmt.Println(Format.Warn(args...))
	}
}

// WarnOnce logs a warning only once per unique message.
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

	fmt.Println(Format.Warn(args...))
}

// Info logs an info message if level allows
func (l *logOps) Info(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Println(Format.Info(args...))
	}
}

// Event logs an event message (same level as info)
func (l *logOps) Event(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Println(Format.Event(args...))
	}
}

// Wait logs a wait message (same level as info)
func (l *logOps) Wait(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Println(Format.Wait(args...))
	}
}

// Ready logs a ready message (same level as info)
func (l *logOps) Ready(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Println(Format.Ready(args...))
	}
}

// Debug logs a debug message if level allows
func (l *logOps) Debug(args ...any) {
	if l.shouldLog(logDebug) {
		fmt.Println(Format.Debug(args...))
	}
}

// Trace logs a trace message if level allows
func (l *logOps) Trace(args ...any) {
	if l.shouldLog(logTrace) {
		fmt.Println(Format.Trace(args...))
	}
}

// Bootstrap logs an indented message (for sub-steps)
func (l *logOps) Bootstrap(args ...any) {
	if l.shouldLog(logInfo) {
		fmt.Println("   " + String(args...))
	}
}
