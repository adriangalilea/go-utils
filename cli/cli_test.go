package cli

import (
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func withColour(t *testing.T, on bool) {
	t.Helper()
	was := Colour
	Colour = on
	t.Cleanup(func() { Colour = was })
}

func TestColourAsked(t *testing.T) {
	for _, c := range []struct {
		force, no, term string
		ttys, want      bool
	}{
		{"", "", "xterm", true, true},
		{"", "", "xterm", false, false},
		{"", "1", "xterm", true, false},
		{"", "", "dumb", true, false},
		{"1", "1", "dumb", false, true},
		{"0", "", "xterm", true, false},
		{"false", "", "xterm", true, false},
	} {
		if got := colourAsked(c.force, c.no, c.term, c.ttys); got != c.want {
			t.Errorf("colourAsked(%q, %q, %q, %v) = %v, want %v", c.force, c.no, c.term, c.ttys, got, c.want)
		}
	}
}

func TestTableAlignsColouredCells(t *testing.T) {
	withColour(t, true)
	got := Table([][]string{
		{"das", UI.Ok("ejected"), "8s"},
		{"exos-long", UI.Warn("draining"), "12s"},
	}, TableOpts{Right: []bool{false, false, true}})
	lines := strings.Split(got, "\n")
	if Width(lines[0]) != Width(lines[1]) {
		t.Fatalf("rows differ in visible width:\n%s", got)
	}
	if !strings.HasPrefix(lines[0], "das        ") {
		t.Fatalf("first column not padded to the widest cell: %q", lines[0])
	}
}

func TestTablePlainHasNoTrailingSpace(t *testing.T) {
	withColour(t, false)
	got := Table([][]string{{"a", "b"}, {"long", ""}}, TableOpts{Head: []string{"k", "v"}, Indent: 2})
	want := "  k     v\n  a     b\n  long"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestClip(t *testing.T) {
	withColour(t, true)
	if got := Clip("hello", 10); got != "hello" {
		t.Fatalf("short string changed: %q", got)
	}
	if got := Clip("hello world", 6); Width(got) != 6 || !strings.HasSuffix(got, "…") {
		t.Fatalf("plain clip: %q (width %d)", got, Width(got))
	}
	got := Clip(UI.Bad("hello world"), 6)
	if Width(got) != 6 || !strings.HasSuffix(got, reset) {
		t.Fatalf("styled clip must keep width and close its style: %q", got)
	}
	if got := Clip("日本語テキスト", 5); Width(got) > 5 {
		t.Fatalf("wide characters straddle the cut: %q (width %d)", got, Width(got))
	}
}

func TestBar(t *testing.T) {
	withColour(t, false)
	for _, c := range []struct {
		done, total int64
		want        string
	}{
		{0, 10, "░░░░░░░░░░"},
		{5, 10, "█████░░░░░"},
		{20, 10, "██████████"},
		{3, 0, "░░░░░░░░░░"},
	} {
		if got := Bar(c.done, c.total, 10, nil); got != c.want {
			t.Errorf("Bar(%d, %d) = %q, want %q", c.done, c.total, got, c.want)
		}
	}
}

func TestDuration(t *testing.T) {
	for _, c := range []struct {
		d    time.Duration
		want string
	}{
		{400 * time.Millisecond, "0.4s"},
		{9940 * time.Millisecond, "9.9s"},
		{12 * time.Second, "12s"},
		{65 * time.Second, "1m05s"},
		{62 * time.Minute, "1h02m"},
	} {
		if got := Duration(c.d); got != c.want {
			t.Errorf("Duration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// Off a terminal a region repaints nothing: Print prints, Done prints the
// final frame once.
func TestRegionOffTerminal(t *testing.T) {
	rd, wr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	r := Live(func() string { return "frame" }, LiveOpts{Out: wr})
	r.Print("above")
	r.DoneWith("final")
	wr.Close()
	got, _ := io.ReadAll(rd)
	if string(got) != "above\nfinal\n" {
		t.Fatalf("got %q", got)
	}
}
