package utils

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/adriangalilea/go-utils/internal/ansi"
)

type formatOps struct {
	Currency formatCurrencyOps
}

// Format returns styled strings without printing, for TUI views and CLI
// output: colours, money, percentages, byte counts, currencies. Colour is the
// module's one decision (the same as package cli): both stdout and stderr
// terminals, NO_COLOR / TERM=dumb / FORCE_COLOR override.
var Format = &formatOps{}

func (f *formatOps) Bold(text string) string    { return ansi.Paint(ansi.Bold, text) }
func (f *formatOps) Red(text string) string     { return ansi.Paint(ansi.Red, text) }
func (f *formatOps) Green(text string) string   { return ansi.Paint(ansi.Green, text) }
func (f *formatOps) Yellow(text string) string  { return ansi.Paint(ansi.Yellow, text) }
func (f *formatOps) Blue(text string) string    { return ansi.Paint(ansi.Blue, text) }
func (f *formatOps) Magenta(text string) string { return ansi.Paint(ansi.Magenta, text) }
func (f *formatOps) Cyan(text string) string    { return ansi.Paint(ansi.Cyan, text) }
func (f *formatOps) Gray(text string) string    { return ansi.Paint(ansi.Gray, text) }

// Green positive, red negative, gray zero.
func signColored(value float64, s string) string {
	switch {
	case value > 0:
		return ansi.Paint(ansi.Green, s)
	case value < 0:
		return ansi.Paint(ansi.Red, s)
	}
	return ansi.Paint(ansi.Gray, s)
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

// GB is the gigabyte Bytes prints: a size stated in GB (a config's
// budget_gb, a disk floor) converts with n * GB, so the figure it prints is
// the figure that was stated.
const GB int64 = 1_000_000_000

// Bytes: 850_000_000_000 -> "850.0 GB", 12_700_000_000_000 -> "12.7 TB",
// 500_000_000 -> "500.0 MB", 4096 -> "4096 B". Decimal SI prefixes, the way
// drives, SMART tools and `df -H` quote capacities; below 1 MB the raw byte
// count is the honest answer. The twin of @adriangalilea/utils `bytes()`.
func (f *formatOps) Bytes(n int64) string {
	Assert(n >= 0, "negative byte count:", n)
	v := float64(n)
	switch {
	case v < 1e6:
		return strconv.FormatInt(n, 10) + " B"
	case v < 1e9:
		return oneDecimal(v/1e6) + " MB"
	case v < 1e12:
		return oneDecimal(v/1e9) + " GB"
	case v < 1e15:
		return oneDecimal(v/1e12) + " TB"
	}
	return oneDecimal(v/1e15) + " PB"
}

func oneDecimal(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

func Bytes(n int64) string {
	return Format.Bytes(n)
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
// The library's one conversion primitive: Assert, Panic messages and KEV
// build their text through it.
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
