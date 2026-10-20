package cli

import (
	"bytes"
	"cmp"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/adriangalilea/go-utils" //nolint:staticcheck
	"github.com/charmbracelet/x/term"
)

const (
	// Synchronized update: modern terminals (Ghostty, iTerm2, kitty, WezTerm,
	// recent Terminal.app) paint the wrapped write at once, so an erase and
	// its repaint never flicker. Others ignore the unknown mode.
	syncOn  = "\x1b[?2026h"
	syncOff = "\x1b[?2026l"
)

// wipe moves to the first line of an n-line region and erases to the end of
// the screen.
func wipe(n int) string {
	if n == 0 {
		return ""
	}
	return "\x1b[" + String(n) + "F\x1b[J"
}

// LiveOpts shapes a live region. The zero value repaints stderr 12.5 times a
// second and prints nothing between frames off a terminal.
type LiveOpts struct {
	Out *os.File // default os.Stderr: stdout stays data for pipes
	FPS float64  // repaints per second on a terminal (0 = 12.5, the spinner's cadence)
	// Heartbeat, off a terminal, prints the current frame every period so a
	// long run in CI or a log file is not silent. 0 = only the final frame.
	Heartbeat time.Duration
}

// Region is a block of lines pinned at the bottom of the terminal, repainted
// from render until Done or Clear. You own the state, the region owns the
// screen: render reads your state and returns the frame, a plain string that
// Table, KV, Bar and Spin compose into.
//
// Nothing else can tear it while it is up: log events (Log.*) print above it,
// so does Print, and so does anything written to stdout (fmt.Println
// included) when stdout is the same terminal — the region holds os.Stdout for
// its lifetime and prints each line above itself. Stdout redirected to a file
// or a pipe is left alone: that is data.
//
// The region never touches signals and never hides the cursor, so Ctrl-C is
// whatever the program makes of it and leaves the terminal as it was, the
// last frame on screen. A program that handles the signal ends the region
// with Done or Clear on its way out.
//
// Off a terminal nothing repaints: Print prints, Done prints the final frame
// once, Heartbeat prints snapshots. The same calling code serves both.
//
// One region at a time: two cannot share the bottom of one screen. render
// runs under the region's lock and must not log or print.
type Region struct {
	mu         sync.Mutex
	render     func() string
	out        *os.File
	tty        bool
	painted    int
	stopped    bool
	quit       chan struct{}
	restoreLog func()
	// While stdout is held: the terminal it was, the pipe it is, and the
	// reader that prints each line above the region.
	stdout  *os.File
	pipe    *os.File
	drained chan struct{}
}

var active atomic.Pointer[Region]

// Live starts a region repainting render (see Region).
func Live(render func() string, o LiveOpts) *Region {
	r := &Region{render: render, out: cmp.Or(o.Out, os.Stderr), quit: make(chan struct{})}
	r.tty = term.IsTerminal(r.out.Fd())
	if !r.tty {
		if o.Heartbeat > 0 {
			r.tick(o.Heartbeat, func() {
				r.mu.Lock()
				defer r.mu.Unlock()
				if !r.stopped {
					r.write(r.render() + "\n")
				}
			})
		}
		return r
	}
	Assert(active.CompareAndSwap(nil, r), "one live region at a time — compose into a single render")
	r.restoreLog = LogThrough(aboveWriter{r})
	if term.IsTerminal(os.Stdout.Fd()) {
		r.holdStdout()
	}
	r.Refresh()
	fps := cmp.Or(o.FPS, 12.5)
	r.tick(time.Duration(float64(time.Second)/fps), r.Refresh)
	return r
}

// Refresh repaints now: the state changed and the next tick is too far.
func (r *Region) Refresh() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tty && !r.stopped {
		r.paint()
	}
}

// Update swaps the render function (a new phase of the same operation).
func (r *Region) Update(render func() string) {
	r.mu.Lock()
	r.render = render
	r.mu.Unlock()
	r.Refresh()
}

// Print writes line above the region, which repaints under it.
func (r *Region) Print(line string) {
	r.above(r.out, line+"\n")
}

// Done stops the region and leaves its current frame in the scrollback.
func (r *Region) Done() { r.DoneWith("") }

// DoneWith stops the region and leaves frame in the scrollback in its place
// ("" = the current frame).
func (r *Region) DoneWith(frame string) {
	r.releaseStdout()
	r.mu.Lock()
	defer r.mu.Unlock()
	if frame == "" {
		frame = r.render()
	}
	r.stop()
	if !r.tty {
		r.write(frame + "\n")
		return
	}
	r.write(wipe(r.painted) + frame + "\n")
}

// Clear stops the region and removes it from the screen.
func (r *Region) Clear() {
	r.releaseStdout()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stop()
	if r.tty {
		r.write(wipe(r.painted))
	}
}

// paint draws the frame. Each line is clipped to the terminal's width (a
// wrapped line would break the erase arithmetic) and a frame taller than the
// screen keeps its bottom, the latest activity. Called with mu held.
func (r *Region) paint() {
	cols, rows, err := term.GetSize(r.out.Fd())
	if err != nil || cols == 0 || rows == 0 {
		cols, rows = 80, 24 // a sizeless pty (script, some CI emulators)
	}
	lines := strings.Split(strings.TrimRight(r.render(), "\n"), "\n")
	for i, l := range lines {
		lines[i] = Clip(l, cols-1)
	}
	if len(lines) >= rows {
		lines = lines[len(lines)-(rows-1):]
	}
	r.write(syncOn + wipe(r.painted) + strings.Join(lines, "\n") + "\n" + syncOff)
	r.painted = len(lines)
}

// above erases the region, writes text to dst, and repaints underneath it, in
// one synchronized update. dst is the region's stream or the held stdout:
// both are the same terminal, written in order.
func (r *Region) above(dst *os.File, text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.tty || r.stopped {
		_, _ = dst.WriteString(text)
		return
	}
	r.write(syncOn + wipe(r.painted))
	r.painted = 0
	_, _ = dst.WriteString(text)
	r.paint()
}

// aboveWriter is the logger's writer while a region is up: one event per Write.
type aboveWriter struct{ r *Region }

func (w aboveWriter) Write(p []byte) (int, error) {
	w.r.above(w.r.out, string(p))
	return len(p), nil
}

// holdStdout points os.Stdout at a pipe whose reader prints every complete
// line above the region onto the real terminal. A program that writes to
// stdout from goroutines still running at Done races the swap back; finish
// them first.
func (r *Region) holdStdout() {
	rd, wr, err := os.Pipe()
	Check(err)
	r.stdout, r.pipe, r.drained = os.Stdout, wr, make(chan struct{})
	os.Stdout = wr
	go func() {
		defer close(r.drained)
		var pending []byte
		buf := make([]byte, 32<<10)
		for {
			n, err := rd.Read(buf)
			pending = append(pending, buf[:n]...)
			if i := bytes.LastIndexByte(pending, '\n'); i >= 0 {
				r.above(r.stdout, string(pending[:i+1]))
				pending = append(pending[:0], pending[i+1:]...)
			}
			if err != nil {
				if len(pending) > 0 {
					r.above(r.stdout, string(pending)+"\n")
				}
				_ = rd.Close()
				return
			}
		}
	}()
}

// releaseStdout gives os.Stdout back and waits until every line written
// before it has printed. Called without mu: the reader needs it.
func (r *Region) releaseStdout() {
	if r.pipe == nil {
		return
	}
	os.Stdout = r.stdout
	_ = r.pipe.Close()
	<-r.drained
	r.pipe = nil
}

// stop ends the ticker and the log routing. Called with mu held: a tick
// waiting on mu finds the region stopped and paints nothing.
func (r *Region) stop() {
	Assert(!r.stopped, "live region already stopped")
	r.stopped = true
	close(r.quit)
	if !r.tty {
		return
	}
	r.restoreLog()
	active.Store(nil)
}

func (r *Region) tick(every time.Duration, fn func()) {
	t := time.NewTicker(every)
	go func() {
		defer t.Stop()
		for {
			select {
			case <-r.quit:
				return
			case <-t.C:
				fn()
			}
		}
	}()
}

// write is the region's one raw write: terminal UI, never a log event.
func (r *Region) write(s string) {
	_, _ = r.out.WriteString(s)
}
