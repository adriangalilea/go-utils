// Package ansi is the one colour decision of the module and the escapes it
// paints with. Format (the root package) and cli both read it, so a program
// is never coloured by one and plain by the other in the same pipe.
package ansi

import (
	"os"

	"github.com/charmbracelet/x/term"
)

const (
	Reset   = "\x1b[0m"
	Bold    = "\x1b[1m"
	Dim     = "\x1b[2m"
	Red     = "\x1b[31m"
	Green   = "\x1b[32m"
	Yellow  = "\x1b[33m"
	Blue    = "\x1b[34m"
	Magenta = "\x1b[35m"
	Cyan    = "\x1b[36m"
	Gray    = "\x1b[90m"
)

// Enabled: does this process paint? Static output goes to stdout and live
// regions to stderr, and any redirection means some string may land in a
// file, so both must be terminals. FORCE_COLOR, NO_COLOR and TERM=dumb
// override (the rule of ts-utils' cli/text).
var Enabled = Asked(os.Getenv("FORCE_COLOR"), os.Getenv("NO_COLOR"), os.Getenv("TERM"),
	term.IsTerminal(os.Stdout.Fd()) && term.IsTerminal(os.Stderr.Fd()))

// Asked is the decision from its inputs.
func Asked(force, no, termName string, ttys bool) bool {
	if force != "" {
		return force != "0" && force != "false"
	}
	if no != "" || termName == "dumb" {
		return false
	}
	return ttys
}

// Paint wraps s in open … Reset when the process paints, else returns it as
// is. Enabled is read per call, so a test can flip it.
func Paint(open, s string) string {
	if !Enabled || s == "" {
		return s
	}
	return open + s + Reset
}
