package cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/creack/pty"
)

var escapes = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// On a real terminal the region holds os.Stdout and os.Stderr: every line
// written to either prints above it (never inside the frame), a line written
// in pieces prints whole, a last line with no newline prints at release, and
// both streams are given back at Done.
func TestRegionHoldsTheTerminal(t *testing.T) {
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skip("no pty here:", err)
	}
	defer ptmx.Close()
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 24, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	screen := make(chan string)
	go func() {
		b, _ := io.ReadAll(ptmx) // EIO once the tty side closes: the screen is whole
		screen <- string(b)
	}()

	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = tty, tty
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()

	r := Live(func() string { return "FRAME" }, LiveOpts{})
	if os.Stdout == tty || os.Stderr == tty {
		t.Fatal("the region did not hold the terminal's streams")
	}
	fmt.Println("to stdout")
	fmt.Fprintln(os.Stderr, "to stderr")
	fmt.Print("in ")
	fmt.Print("pieces\n")
	fmt.Print("no newline")
	r.DoneWith("FINAL")
	if os.Stdout != tty || os.Stderr != tty {
		t.Fatal("Done did not give the streams back")
	}
	tty.Close()

	raw := strings.ReplaceAll(<-screen, "\r", "")
	// Above the region = written right after the region erased itself. A
	// line that bypassed the hold lands under the frame and the next repaint
	// wipes it off the screen, so its text would still be in the stream: the
	// erase in front of it is the proof.
	above := map[string]bool{} // every line of plain text that follows an erase
	for _, part := range strings.Split(raw, wipe(1))[1:] {
		block, _, _ := strings.Cut(part, "\x1b")
		for _, l := range strings.Split(block, "\n") {
			above[l] = true
		}
	}
	for _, want := range []string{"to stdout", "to stderr", "in pieces", "no newline"} {
		if !above[want] {
			t.Errorf("%q was not printed above the region; stream: %q", want, raw)
		}
	}
	lines := strings.Split(strings.TrimRight(escapes.ReplaceAllString(raw, ""), "\n"), "\n")
	if lines[len(lines)-1] != "FINAL" {
		t.Errorf("the final frame is not the last line; stream: %q", raw)
	}
}
