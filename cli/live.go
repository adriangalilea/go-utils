package cli

import (
	"cmp"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	. "github.com/adriangalilea/go-utils" //nolint:staticcheck
	"github.com/charmbracelet/x/term"
)

const (
	hideCursor = "\x1b[?25l"
	showCursor = "\x1b[?25h"
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
// second, prints nothing between frames off a terminal, and restores the
// cursor before dying on Ctrl-C.
type LiveOpts struct {
	Out *os.File // default os.Stderr: stdout stays data for pipes
	FPS float64  // repaints per second on a terminal (0 = 12.5, the spinner's cadence)
	// Heartbeat, off a terminal, prints the current frame every period so a
	// long run in CI or a log file is not silent. 0 = only the final frame.
	Heartbeat time.Duration
	// AppHandlesSignals leaves SIGINT and SIGTERM alone: the program has its
	// own shutdown and calls Done or Clear on the way out. Otherwise the
	// region restores the cursor and the signal kills the program as usual.
	AppHandlesSignals bool
}

// Region is a block of lines pinned at the bottom of the terminal, repainted
// from render until Done or Clear. You own the state, the region owns the
// screen: render reads your state and returns the frame, a plain string that
// Table, KV, Bar and Spin compose into.
//
// While it is up, log events (Log.*) print above it instead of through it,
// and so does Print: write every other line with Print, because fmt.Println
// to the same terminal would land inside the region and tear it.
//
// Off a terminal nothing repaints: Print prints, Done prints the final frame
// once, Heartbeat prints snapshots. The same calling code serves both.
//
// One region at a time: two cannot share the bottom of one screen. render
// runs under the region's lock and must not log.
type Region struct {
	mu         sync.Mutex
	render     func() string
	out        *os.File
	tty        bool
	painted    int
	stopped    bool
	quit       chan struct{}
	restoreLog func()
	sigs       chan os.Signal
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
	if !o.AppHandlesSignals {
		r.guardSignals()
	}
	r.mu.Lock()
	r.write(hideCursor)
	r.paint()
	r.mu.Unlock()
	fps := cmp.Or(o.FPS, 12.5)
	r.tick(time.Duration(float64(time.Second)/fps), func() { r.Refresh() })
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
	r.above(line + "\n")
}

// Done stops the region and leaves its current frame in the scrollback.
func (r *Region) Done() { r.DoneWith("") }

// DoneWith stops the region and leaves frame in the scrollback in its place
// ("" = the current frame).
func (r *Region) DoneWith(frame string) {
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
	r.write(wipe(r.painted) + frame + "\n" + showCursor)
}

// Clear stops the region and removes it from the screen.
func (r *Region) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stop()
	if r.tty {
		r.write(wipe(r.painted) + showCursor)
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

// above erases the region, writes text, and repaints underneath it, all in one
// synchronized update.
func (r *Region) above(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.tty || r.stopped {
		r.write(text)
		return
	}
	r.write(syncOn + wipe(r.painted) + text)
	r.painted = 0
	r.paint()
}

// aboveWriter is the logger's writer while a region is up: one event per Write.
type aboveWriter struct{ r *Region }

func (w aboveWriter) Write(p []byte) (int, error) {
	w.r.above(string(p))
	return len(p), nil
}

// stop ends the ticker, the log routing and the signal guard. Called with mu
// held: a tick waiting on mu finds the region stopped and paints nothing.
func (r *Region) stop() {
	Assert(!r.stopped, "live region already stopped")
	r.stopped = true
	close(r.quit)
	if !r.tty {
		return
	}
	r.restoreLog()
	if r.sigs != nil {
		signal.Stop(r.sigs)
	}
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

// guardSignals restores the cursor on SIGINT/SIGTERM, then lets the signal do
// what it would have done without us: stop listening and raise it again.
func (r *Region) guardSignals() {
	r.sigs = make(chan os.Signal, 1)
	signal.Notify(r.sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-r.quit:
		case sig := <-r.sigs:
			r.mu.Lock()
			r.write("\n" + showCursor)
			signal.Stop(r.sigs)
			r.mu.Unlock()
			_ = syscall.Kill(os.Getpid(), sig.(syscall.Signal))
		}
	}()
}

// write is the region's one raw write: terminal UI, never a log event.
func (r *Region) write(s string) {
	_, _ = r.out.WriteString(s)
}
