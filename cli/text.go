// Package cli is terminal presentation, the twin of @adriangalilea/utils/cli:
// a semantic palette, ANSI-aware width/clip/pad, aligned tables and key/value
// blocks, and a live region that repaints in place (spinner, progress bar,
// elapsed time) with log events printed above it.
//
// Everything returns strings; only a live region writes. Colour is one
// decision for the whole process: both stdout and stderr must be terminals
// (static output goes to stdout, live regions to stderr, and any redirection
// means some string may land in a file), NO_COLOR, TERM=dumb and FORCE_COLOR
// override.
//
//	fmt.Println(cli.Table([][]string{
//		{"das", cli.UI.Ok("ejected")},
//		{"exos", cli.UI.Warn("draining")},
//	}, cli.TableOpts{Head: []string{"location", "state"}}))
package cli

import (
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

const reset = "\x1b[0m"

// Colour reports whether this process paints (see the package doc).
var Colour = colourAsked(os.Getenv("FORCE_COLOR"), os.Getenv("NO_COLOR"), os.Getenv("TERM"),
	term.IsTerminal(os.Stdout.Fd()) && term.IsTerminal(os.Stderr.Fd()))

func colourAsked(force, no, termName string, ttys bool) bool {
	if force != "" {
		return force != "0" && force != "false"
	}
	if no != "" || termName == "dumb" {
		return false
	}
	return ttys
}

// Style paints a string, or returns it as is when the process does not colour.
type Style func(string) string

func paint(open string) Style {
	return func(s string) string {
		if !Colour || s == "" {
			return s
		}
		return open + s + reset
	}
}

var (
	Bold   = paint("\x1b[1m")
	Dim    = paint("\x1b[2m")
	Red    = paint("\x1b[31m")
	Green  = paint("\x1b[32m")
	Yellow = paint("\x1b[33m")
	Cyan   = paint("\x1b[36m")
	Gray   = paint("\x1b[90m")
)

// UI is the semantic palette, the same roles as ts-utils' ui: use these, not
// raw colours, so intent reads the same in every program.
var UI = struct {
	Head, Accent, Muted, Ok, Warn, Bad, Ref Style
}{
	Head:   Bold,
	Accent: Cyan,
	Muted:  Dim,
	Ok:     Green,
	Warn:   Yellow,
	Bad:    Red,
	Ref:    Gray,
}

// Link is text linked to url (OSC 8) where this process colours: clickable in
// a terminal that knows hyperlinks, the words alone everywhere else.
func Link(text, url string) string {
	if !Colour {
		return text
	}
	return ansi.SetHyperlink(url) + text + ansi.ResetHyperlink()
}

// Width is the visible width in terminal cells: escapes stripped, grapheme
// clusters measured (a wide character or an emoji takes two).
func Width(s string) int { return ansi.StringWidth(s) }

// Clip truncates to n visible cells with an ellipsis at the cut. Escapes pass
// through, and a style left open by the cut is closed.
func Clip(s string, n int) string {
	if Width(s) <= n {
		return s
	}
	c := ansi.Truncate(s, n, "…")
	if strings.Contains(c, "\x1b[") && !strings.HasSuffix(c, reset) {
		c += reset
	}
	return c
}

// PadEnd pads s with spaces to n visible cells.
func PadEnd(s string, n int) string { return s + strings.Repeat(" ", max(0, n-Width(s))) }

// PadStart pads s on the left to n visible cells.
func PadStart(s string, n int) string { return strings.Repeat(" ", max(0, n-Width(s))) + s }
