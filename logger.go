package utils

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// LogLevel for filtering output
type LogLevel int

const (
	LogSilent LogLevel = iota
	LogError
	LogWarn
	LogInfo
	LogDebug
	LogTrace
)

// logOps handles all logging operations with level filtering
type logOps struct {
	mu       sync.Mutex
	warnOnce map[string]struct{}
}

// Log provides logging operations with level filtering
var Log = &logOps{
	warnOnce: make(map[string]struct{}),
}

// getLevel returns the current log level from KEV
func (l *logOps) getLevel() LogLevel {
	level := strings.ToLower(KEV.Get("LOG_LEVEL", "info"))
	switch level {
	case "silent":
		return LogSilent
	case "error":
		return LogError
	case "warn", "warning":
		return LogWarn
	case "info":
		return LogInfo
	case "debug":
		return LogDebug
	case "trace":
		return LogTrace
	default:
		return LogInfo
	}
}

// shouldLog checks if message should be logged at given level
func (l *logOps) shouldLog(msgLevel LogLevel) bool {
	currentLevel := l.getLevel()
	return msgLevel <= currentLevel
}

// Error logs an error message to stderr (always logs unless silent)
func (l *logOps) Error(args ...interface{}) {
	if l.shouldLog(LogError) {
		fmt.Fprintln(os.Stderr, Format.Error(args...))
	}
}

// Warn logs a warning message if level allows
func (l *logOps) Warn(args ...interface{}) {
	if l.shouldLog(LogWarn) {
		fmt.Println(Format.Warn(args...))
	}
}

// WarnOnce logs a warning only once per unique message
func (l *logOps) WarnOnce(args ...interface{}) {
	key := fmt.Sprint(args...)
	
	l.mu.Lock()
	if _, exists := l.warnOnce[key]; exists {
		l.mu.Unlock()
		return
	}
	l.warnOnce[key] = struct{}{}
	l.mu.Unlock()
	
	if l.shouldLog(LogWarn) {
		fmt.Println(Format.Warn(args...))
	}
}

// Info logs an info message if level allows
func (l *logOps) Info(args ...interface{}) {
	if l.shouldLog(LogInfo) {
		fmt.Println(Format.Info(args...))
	}
}

// Event logs an event message (same level as info)
func (l *logOps) Event(args ...interface{}) {
	if l.shouldLog(LogInfo) {
		fmt.Println(Format.Event(args...))
	}
}

// Wait logs a wait message (same level as info)
func (l *logOps) Wait(args ...interface{}) {
	if l.shouldLog(LogInfo) {
		fmt.Println(Format.Wait(args...))
	}
}

// Ready logs a ready message (same level as info)
func (l *logOps) Ready(args ...interface{}) {
	if l.shouldLog(LogInfo) {
		fmt.Println(Format.Ready(args...))
	}
}

// Debug logs a debug message if level allows
func (l *logOps) Debug(args ...interface{}) {
	if l.shouldLog(LogDebug) {
		fmt.Println(Format.Info(args...))
	}
}

// Trace logs a trace message if level allows
func (l *logOps) Trace(args ...interface{}) {
	if l.shouldLog(LogTrace) {
		fmt.Println(Format.Trace(args...))
	}
}

// Bootstrap logs an indented message (for sub-steps)
func (l *logOps) Bootstrap(args ...interface{}) {
	if l.shouldLog(LogInfo) {
		message := fmt.Sprintln(args...)
		message = message[:len(message)-1] // Remove trailing newline
		fmt.Println("   " + message)
	}
}