package utils

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	errorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1"))
	warnStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	eventStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	traceStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
	waitStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))
	infoStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("7"))
	readyStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	debugStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("8"))

	boldStyle    = lipgloss.NewStyle().Bold(true)
	redStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	greenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	blueStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	magentaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	cyanStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	grayStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type messageType int

const (
	msgTrace messageType = iota
	msgDebug
	msgInfo
	msgEvent
	msgWait
	msgReady
	msgWarn
	msgError
)

var symbols = map[messageType]string{
	msgWait:  "○",
	msgError: "⨯",
	msgWarn:  "⚠",
	msgReady: "▶",
	msgInfo:  " ",
	msgEvent: "✓",
	msgDebug: "◦",
	msgTrace: "»",
}

var symbolStyles = map[messageType]lipgloss.Style{
	msgWait:  waitStyle,
	msgError: errorStyle,
	msgWarn:  warnStyle,
	msgReady: readyStyle,
	msgInfo:  infoStyle,
	msgEvent: eventStyle,
	msgDebug: debugStyle,
	msgTrace: traceStyle,
}

type formatOps struct {
	colorEnabled bool

	Currency formatCurrencyOps
}

// Format returns styled strings without printing - built for TUI views
// (Bubbletea) and for Log, which handles the actual output.
var Format = &formatOps{}

func init() {
	Format.colorEnabled = shouldEnableColor()
}

// NO_COLOR and FORCE_COLOR env vars override; lipgloss handles terminal detection.
func shouldEnableColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	return true
}

func (f *formatOps) formatMessage(msgType messageType, args ...any) string {
	message := String(args...)

	symbol := symbols[msgType]
	if f.colorEnabled {
		symbol = symbolStyles[msgType].Render(symbol)
	}

	return symbol + " " + message
}

func (f *formatOps) Error(args ...any) string {
	return f.formatMessage(msgError, args...)
}

func (f *formatOps) Warn(args ...any) string {
	return f.formatMessage(msgWarn, args...)
}

func (f *formatOps) Info(args ...any) string {
	return f.formatMessage(msgInfo, args...)
}

func (f *formatOps) Wait(args ...any) string {
	return f.formatMessage(msgWait, args...)
}

func (f *formatOps) Ready(args ...any) string {
	return f.formatMessage(msgReady, args...)
}

func (f *formatOps) Event(args ...any) string {
	return f.formatMessage(msgEvent, args...)
}

func (f *formatOps) Debug(args ...any) string {
	return f.formatMessage(msgDebug, args...)
}

func (f *formatOps) Trace(args ...any) string {
	return f.formatMessage(msgTrace, args...)
}

func Error(args ...any) string {
	return Format.Error(args...)
}

func Warn(args ...any) string {
	return Format.Warn(args...)
}

func Info(args ...any) string {
	return Format.Info(args...)
}

func Wait(args ...any) string {
	return Format.Wait(args...)
}

func Ready(args ...any) string {
	return Format.Ready(args...)
}

func Event(args ...any) string {
	return Format.Event(args...)
}

func Debug(args ...any) string {
	return Format.Debug(args...)
}

func Trace(args ...any) string {
	return Format.Trace(args...)
}

func (f *formatOps) Bold(text string) string {
	return boldStyle.Render(text)
}

func (f *formatOps) Red(text string) string {
	return redStyle.Render(text)
}

func (f *formatOps) Green(text string) string {
	return greenStyle.Render(text)
}

func (f *formatOps) Yellow(text string) string {
	return yellowStyle.Render(text)
}

func (f *formatOps) Blue(text string) string {
	return blueStyle.Render(text)
}

func (f *formatOps) Magenta(text string) string {
	return magentaStyle.Render(text)
}

func (f *formatOps) Cyan(text string) string {
	return cyanStyle.Render(text)
}

func (f *formatOps) Gray(text string) string {
	return grayStyle.Render(text)
}

// Green positive, red negative, gray zero. Respects color detection.
func signColored(value float64, s string) string {
	if !Format.colorEnabled {
		return s
	}
	if value > 0 {
		return greenStyle.Render(s)
	}
	if value < 0 {
		return redStyle.Render(s)
	}
	return grayStyle.Render(s)
}

// "1234567.89" -> "1,234,567.89". Sign prefixes pass through untouched.
func groupThousands(formatted string) string {
	sign := ""
	if strings.HasPrefix(formatted, "-") || strings.HasPrefix(formatted, "+") {
		sign, formatted = formatted[:1], formatted[1:]
	}

	intPart, rest := formatted, ""
	if i := strings.IndexByte(formatted, '.'); i >= 0 {
		intPart, rest = formatted[:i], formatted[i:]
	}
	if len(intPart) <= 3 {
		return sign + formatted
	}

	var b strings.Builder
	for i, digit := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(digit)
	}
	return sign + b.String() + rest
}

func (f *formatOps) Money(value float64) string {
	return signColored(value, f.MoneyPlain(value))
}

// -1234.5 -> "-$1,234.50", 1234.5 -> "+$1,234.50"
func (f *formatOps) MoneyPlain(value float64) string {
	formatted := "$" + groupThousands(fmt.Sprintf("%.2f", math.Abs(value)))
	if value > 0 {
		return "+" + formatted
	}
	if value < 0 {
		return "-" + formatted
	}
	return formatted
}

func (f *formatOps) Percent(value float64) string {
	return signColored(value, fmt.Sprintf("%.1f%%", value))
}

func (f *formatOps) Number(value float64, decimals int) string {
	formatted := fmt.Sprintf("%.*f", decimals, value)
	if value > 0 {
		formatted = "+" + formatted
	}
	return signColored(value, formatted)
}

func Money(value float64) string {
	return Format.Money(value)
}

func MoneyPlain(value float64) string {
	return Format.MoneyPlain(value)
}

func Percent(value float64) string {
	return Format.Percent(value)
}

func Number(value float64, decimals int) string {
	return Format.Number(value, decimals)
}

// String converts any values to a single string, space-separated.
// The library's one conversion primitive: Assert, Format, Log and KEV
// all build their messages through it.
//
//	String(42)                    // "42"
//	String("port:", 8080)         // "port: 8080"
func String(args ...any) string {
	s := fmt.Sprintln(args...)
	return s[:len(s)-1]
}

type formatCurrencyOps struct{}

func (fc formatCurrencyOps) USD(value float64) string {
	return fc.Auto(value, "USD")
}

func (fc formatCurrencyOps) BTC(value float64) string {
	return fc.Auto(value, "BTC")
}

func (fc formatCurrencyOps) ETH(value float64) string {
	return fc.Auto(value, "ETH")
}

func (fc formatCurrencyOps) Auto(value float64, currencyCode string) string {
	formatted := fc.Plain(value, currencyCode)
	if value > 0 {
		formatted = "+" + formatted
	}
	return signColored(value, formatted)
}

// Symbol before the amount for fiat/stablecoins, after for crypto.
func (fc formatCurrencyOps) Plain(value float64, currencyCode string) string {
	decimals := Currency.GetOptimalDecimals(value, currencyCode)
	symbol := Currency.GetSymbol(currencyCode)

	if Currency.IsFiat(currencyCode) || Currency.IsStablecoin(currencyCode) {
		formatted := symbol + groupThousands(fmt.Sprintf("%.*f", decimals, math.Abs(value)))
		if value < 0 {
			formatted = "-" + formatted
		}
		return formatted
	}

	return groupThousands(fmt.Sprintf("%.*f", decimals, value)) + " " + symbol
}

func (fc formatCurrencyOps) Percentage(value float64) string {
	decimals := 1
	if math.Abs(value) < 0.1 {
		decimals = 2
	} else if math.Abs(value) >= 100 {
		decimals = 0
	}

	formatted := fmt.Sprintf("%.*f%%", decimals, value)
	if value > 0 {
		formatted = "+" + formatted
	}
	return signColored(value, formatted)
}

func (fc formatCurrencyOps) PercentageChange(oldValue, newValue float64) string {
	return fc.Percentage(Currency.PercentageChange(oldValue, newValue))
}
