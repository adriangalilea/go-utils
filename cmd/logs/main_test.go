package main

import (
	"bufio"
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/adriangalilea/go-utils/internal/stream"
)

// render runs the fixture through the viewer: human when human, else the
// grep lines a pipe gets.
func render(t *testing.T, human bool, keep filter) string {
	t.Helper()
	f, err := os.Open("testdata/fixture.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	v := &viewer{w: w, keep: keep}
	if human {
		v.human = stream.NewHuman(&buf)
	}
	v.read(f)
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

var all = filter{level: stream.LevelTrace}

// The golden files are the renderings themselves: events from two services
// (a Go daemon, a TypeScript worker), a failed span with a stack, a Go panic
// line and a JSON line that is not an event, both passed through.
func TestGolden(t *testing.T) {
	time.Local = time.UTC
	for _, c := range []struct {
		human  bool
		golden string
	}{{false, "testdata/fixture.grep"}, {true, "testdata/fixture.human"}} {
		want, err := os.ReadFile(c.golden)
		if err != nil {
			t.Fatal(err)
		}
		if got := render(t, c.human, all); got != string(want) {
			t.Errorf("%s\n got:\n%s\nwant:\n%s", c.golden, got, want)
		}
	}
}

// Each filter keeps the events it names, by their time stamps in the
// fixture, and never the lines that are not events.
func TestFilters(t *testing.T) {
	cases := []struct {
		keep  filter
		times string
	}{
		{filter{level: stream.LevelWarn}, "39.123 39.200"},
		{filter{level: stream.LevelInfo}, "39.100 39.101 39.123 39.124 39.200 39.355 41.000"},
		{filter{level: stream.LevelTrace, scope: []string{"payments"}}, "39.101 39.123 39.124 39.200 39.355"},
		{filter{level: stream.LevelTrace, scope: []string{"payments", "stripe"}}, "39.124 39.200"},
		{filter{level: stream.LevelTrace, trace: "4bf92f3577b34da6a3ce929d0e0e4736"}, "39.101 39.123 39.124 39.200 39.355"},
	}
	for _, c := range cases {
		var times, passed []string
		for _, line := range strings.Split(strings.TrimSuffix(render(t, false, c.keep), "\n"), "\n") {
			if strings.HasPrefix(line, "2026-10-12T12:20:") {
				times = append(times, line[len("2026-10-12T12:20:"):len("2026-10-12T12:20:39.123")])
			} else {
				passed = append(passed, line)
			}
		}
		if got := strings.Join(times, " "); got != c.times {
			t.Errorf("%+v kept %s, want %s", c.keep, got, c.times)
		}
		if len(passed) != 2 {
			t.Errorf("%+v must pass both lines that are not events through: %q", c.keep, passed)
		}
	}
}

func TestTailOffset(t *testing.T) {
	path := t.TempDir() + "/app.log"
	var b strings.Builder
	for i := range 15 {
		b.WriteString(strings.Repeat("x", i) + "\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	content := b.String()
	if got := content[tailOffset(f, 10):]; strings.Count(got, "\n") != 10 || !strings.HasPrefix(got, "xxxxx\n") {
		t.Fatalf("last 10 lines: %q", got)
	}
	if got := tailOffset(f, 100); got != 0 {
		t.Fatalf("fewer lines than asked: from the start, got %d", got)
	}
}
