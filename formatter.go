package utils

import (
	"fmt"
	"os"
	"sync"
	
	"github.com/charmbracelet/lipgloss"
)

// Lipgloss styles for each message type
var (
	errorStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	warnStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	eventStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	traceStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	waitStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))
	infoStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))
	readyStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
)

// MessageType represents different types of formatted messages
type MessageType int

const (
	TypeTrace MessageType = iota
	TypeInfo
	TypeEvent
	TypeWait
	TypeReady
	TypeWarn
	TypeError
)

// Unicode symbols for each message type
var symbols = map[MessageType]string{
	TypeWait:    "○",
	TypeError:   "⨯",
	TypeWarn:    "⚠",
	TypeReady:   "▶",
	TypeInfo:    " ",
	TypeEvent:   "✓",
	TypeTrace:   "»",
}

// Config for formatter
type Config struct {
	NoColor    bool
	ForceColor bool
	Prefix     string
}

// formatOps handles all formatting operations
type formatOps struct {
	mu           sync.Mutex
	config       Config
	colorEnabled bool
}

// Format provides string formatting operations
var Format = &formatOps{
	config: Config{},
}

// Initialize Format colors
var _ = func() bool {
	Format.colorEnabled = Format.shouldEnableColor()
	return true
}()

// shouldEnableColor determines if color output should be enabled
func (f *formatOps) shouldEnableColor() bool {
	if f.config.NoColor {
		return false
	}
	if f.config.ForceColor {
		return true
	}
	
	// Check if NO_COLOR env var is set
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	
	// Check if FORCE_COLOR env var is set
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	
	// Lipgloss will handle terminal detection
	return true
}

// formatMessage creates a formatted message with symbol and color
func (f *formatOps) formatMessage(msgType MessageType, args ...interface{}) string {
	// Check if strict mode is enabled
	// Set FORMATTER_STRICT to any value to only accept strings (catches type errors)
	// By default, accepts any type for convenience (like fmt.Println)
	if os.Getenv("FORMATTER_STRICT") != "" {
		// In strict mode, only accept strings
		for i, arg := range args {
			if _, ok := arg.(string); !ok {
				panic(fmt.Sprintf("formatter: argument %d is %T, not string (FORMATTER_STRICT is set)", i, arg))
			}
		}
	}
	
	// Format message - use Sprintln to add spaces between args, then trim newline
	message := fmt.Sprintln(args...)
	message = message[:len(message)-1] // Remove trailing newline
	
	if f.config.Prefix != "" {
		message = fmt.Sprintf("[%s] %s", f.config.Prefix, message)
	}
	
	// Get symbol and apply style
	symbol := symbols[msgType]
	var coloredSymbol string
	
	if !f.colorEnabled {
		coloredSymbol = symbol
	} else {
		switch msgType {
		case TypeError:
			coloredSymbol = errorStyle.Render(symbol)
		case TypeWarn:
			coloredSymbol = warnStyle.Render(symbol)
		case TypeEvent:
			coloredSymbol = eventStyle.Render(symbol)
		case TypeTrace:
			coloredSymbol = traceStyle.Render(symbol)
		case TypeWait:
			coloredSymbol = waitStyle.Render(symbol)
		case TypeInfo:
			coloredSymbol = infoStyle.Render(symbol)
		case TypeReady:
			coloredSymbol = readyStyle.Render(symbol)
		}
	}
	
	return fmt.Sprintf("%s %s", coloredSymbol, message)
}

// Error returns an error-formatted string
func (f *formatOps) Error(args ...interface{}) string {
	return f.formatMessage(TypeError, args...)
}

// Warn returns a warning-formatted string
func (f *formatOps) Warn(args ...interface{}) string {
	return f.formatMessage(TypeWarn, args...)
}

// Info returns an info-formatted string
func (f *formatOps) Info(args ...interface{}) string {
	return f.formatMessage(TypeInfo, args...)
}

// Wait returns a wait-formatted string
func (f *formatOps) Wait(args ...interface{}) string {
	return f.formatMessage(TypeWait, args...)
}

// Ready returns a ready-formatted string
func (f *formatOps) Ready(args ...interface{}) string {
	return f.formatMessage(TypeReady, args...)
}

// Event returns an event-formatted string
func (f *formatOps) Event(args ...interface{}) string {
	return f.formatMessage(TypeEvent, args...)
}

// Trace returns a trace-formatted string
func (f *formatOps) Trace(args ...interface{}) string {
	return f.formatMessage(TypeTrace, args...)
}


// Package-level functions using default formatter

// Error returns an error-formatted string
func Error(args ...interface{}) string {
	return Format.Error(args...)
}

// Warn returns a warning-formatted string
func Warn(args ...interface{}) string {
	return Format.Warn(args...)
}

// Info returns an info-formatted string
func Info(args ...interface{}) string {
	return Format.Info(args...)
}

// Wait returns a wait-formatted string
func Wait(args ...interface{}) string {
	return Format.Wait(args...)
}

// Ready returns a ready-formatted string
func Ready(args ...interface{}) string {
	return Format.Ready(args...)
}

// Event returns an event-formatted string
func Event(args ...interface{}) string {
	return Format.Event(args...)
}

// Trace returns a trace-formatted string
func Trace(args ...interface{}) string {
	return Format.Trace(args...)
}

// Style functions using lipgloss

// Bold returns a function that renders text in bold
func (f *formatOps) Bold(text string) string {
	return lipgloss.NewStyle().Bold(true).Render(text)
}

// Red returns a function that renders text in red
func (f *formatOps) Red(text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(text)
}

// Green returns a function that renders text in green
func (f *formatOps) Green(text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(text)
}

// Yellow returns a function that renders text in yellow
func (f *formatOps) Yellow(text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(text)
}

// Blue returns a function that renders text in blue
func (f *formatOps) Blue(text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Render(text)
}

// Magenta returns a function that renders text in magenta
func (f *formatOps) Magenta(text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(text)
}

// Cyan returns a function that renders text in cyan
func (f *formatOps) Cyan(text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(text)
}

// Gray returns a function that renders text in gray
func (f *formatOps) Gray(text string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(text)
}