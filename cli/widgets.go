package cli

import (
	"strings"
	"sync"
	"time"

	. "github.com/adriangalilea/go-utils" //nolint:staticcheck
)

// The widgets are plain string builders: call them inside a region's render
// and they animate by being re-rendered.

var spinFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spin is the spinner frame for now.
func Spin() string {
	return Cyan(spinFrames[time.Now().UnixMilli()/80%int64(len(spinFrames))])
}

// Bar is a progress bar width cells wide, the filled part in style (nil =
// green; pass UI.Warn for a throttled or retrying state). done past total
// clamps to full: live counters drift, and that is the caller's data. A total
// of 0 is unknown and draws empty, never a fake fill.
func Bar(done, total int64, width int, style Style) string {
	Assert(done >= 0 && total >= 0, "Bar: negative counts", done, total)
	if style == nil {
		style = Green
	}
	fill := 0
	if total > 0 {
		fill = int(min(1, float64(done)/float64(total))*float64(width) + 0.5)
	}
	return style(strings.Repeat("█", fill)) + Dim(strings.Repeat("░", width-fill))
}

// Elapsed is the time since `since`, compact: 0.4s · 12s · 1m05s · 1h02m.
func Elapsed(since time.Time) string { return Duration(time.Since(since)) }

// Duration renders d the way Elapsed does, for an ETA or a total.
func Duration(d time.Duration) string {
	s := d.Seconds()
	if s < 10 {
		return formatTenths(s) + "s"
	}
	r := int(s + 0.5)
	if r < 60 {
		return String(r) + "s"
	}
	m := r / 60
	if m < 60 {
		return String(m) + "m" + pad2(r%60) + "s"
	}
	return String(m/60) + "h" + pad2(m%60) + "m"
}

func formatTenths(s float64) string {
	t := int(s*10 + 0.5)
	return String(t/10) + "." + String(t%10)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + String(n)
	}
	return String(n)
}

// Spinner animates label with a spinner and the elapsed time while fn runs,
// then leaves "✓ label 1.2s" or "⨯ label 1.2s" and returns fn's error. fn
// can relabel mid-flight through set. Off a terminal it prints only the
// final line; under a region already up, the final line prints above it.
func Spinner(label string, fn func(set func(string)) error) error {
	start := time.Now()
	var (
		mu      sync.Mutex
		current = label
	)
	get := func() string {
		mu.Lock()
		defer mu.Unlock()
		return current
	}
	set := func(l string) {
		mu.Lock()
		defer mu.Unlock()
		current = l
	}
	outer := active.Load()
	var r *Region
	if outer == nil {
		r = Live(func() string { return Spin() + " " + get() + " " + Dim(Elapsed(start)) }, LiveOpts{})
	}
	defer func() {
		if p := recover(); p != nil {
			if r != nil {
				r.Clear() // the cursor comes back before the crash prints
			}
			panic(p)
		}
	}()
	err := fn(set)
	mark := Green("✓")
	if err != nil {
		mark = Red("⨯")
	}
	line := mark + " " + get() + " " + Dim(Elapsed(start))
	switch {
	case r != nil:
		r.DoneWith(line)
	case outer != nil:
		outer.Print(line)
	}
	return err
}
