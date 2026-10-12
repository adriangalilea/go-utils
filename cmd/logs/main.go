// logs reads the event stream go-utils, ts-utils and py-utils write and
// renders it: human at a terminal (the logger's own rendering), and in a pipe
// one grep line per event:
//
//	2026-10-12T12:20:39.123Z WARN  [payments] retrying attempt=2
//
// A line that is not an event (a Go panic, net/http's own error log, anything
// a program wrote to stderr by itself) is printed as it is, never dropped,
// whatever the filters.
//
//	logs [-f] [--level L] [--scope S] [--trace ID] [file ...]
//
// No file reads stdin. -f follows one file from its last 10 lines, the way
// tail -f does, and keeps following when it is rotated (replaced or
// truncated). The name is logs because `log` is a zsh builtin and
// /usr/bin/log on macOS.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/adriangalilea/go-utils/internal/stream"
)

func main() {
	keep := filter{level: stream.LevelTrace}
	follow := flag.Bool("f", false, "follow the file as it grows, across rotation")
	flag.Func("level", "show this level and above: error, warn, info, debug, trace", func(s string) error {
		level, ok := stream.ParseLevel(strings.ToLower(s))
		if !ok || level == stream.LevelSilent {
			return errors.New("want error, warn, info, debug or trace")
		}
		keep.level = level
		return nil
	})
	flag.Func("scope", "show this scope path and those under it, space-separated: \"api\" shows [api auth]", func(s string) error {
		keep.scope = strings.Fields(s)
		return nil
	})
	flag.StringVar(&keep.trace, "trace", "", "show one trace id")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: logs [-f] [--level L] [--scope S] [--trace ID] [file ...]")
		flag.PrintDefaults()
	}
	flag.Parse()
	files := flag.Args()
	if *follow && len(files) != 1 {
		fmt.Fprintln(os.Stderr, "logs: -f follows exactly one file")
		os.Exit(2)
	}

	v := &viewer{w: bufio.NewWriter(os.Stdout), keep: keep}
	if stat, err := os.Stdout.Stat(); err == nil && stat.Mode()&os.ModeCharDevice != 0 {
		v.human = stream.NewHuman(os.Stdout)
	}

	switch {
	case *follow:
		followFile(files[0], v)
	case len(files) == 0:
		v.read(os.Stdin)
	default:
		for _, path := range files {
			f, err := os.Open(path)
			if err != nil {
				v.flush()
				fail(err)
			}
			v.read(f)
			check(f.Close())
		}
	}
	v.flush()
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "logs:", err)
	os.Exit(1)
}

type filter struct {
	level stream.Level
	scope []string
	trace string
}

func (f filter) keeps(e *stream.Event) bool {
	return e.Verb.Level() <= f.level &&
		len(e.Scope) >= len(f.scope) && slices.Equal(e.Scope[:len(f.scope)], f.scope) &&
		(f.trace == "" || e.TraceID == f.trace)
}

// viewer renders lines as they come. human nil: grep lines.
type viewer struct {
	w     *bufio.Writer
	human *stream.Human
	keep  filter
}

// line renders one line, without its newline.
func (v *viewer) line(raw []byte) {
	e, ok := stream.Parse(raw)
	switch {
	case !ok:
		v.put(string(raw) + "\n")
	case !v.keep.keeps(&e):
	case v.human != nil:
		v.put(v.human.Render(&e, true))
	default:
		v.put(e.GrepLine() + "\n")
	}
}

// put buffers s. A write error sticks in the bufio.Writer and surfaces at
// the next flush.
func (v *viewer) put(s string) { _, _ = v.w.WriteString(s) }

// flush writes what is buffered. Stdout gone (a closed pipe) ends the viewer.
func (v *viewer) flush() {
	if err := v.w.Flush(); err != nil {
		fail(err)
	}
}

// read renders r to its end, a last line without a newline included.
func (v *viewer) read(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		raw, err := br.ReadBytes('\n')
		if len(raw) > 0 {
			v.line(trimNewline(raw))
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			v.flush()
			fail(err)
		}
	}
}

func trimNewline(raw []byte) []byte {
	if n := len(raw); n > 0 && raw[n-1] == '\n' {
		return raw[:n-1]
	}
	return raw
}

// followFile renders the file's last 10 lines, then what is appended, polling.
// When the path is replaced (a new inode) or the file shrinks (truncated in
// place), it reopens from the start; while the path is missing mid-rotation
// it waits. A partial last line waits for its newline.
func followFile(path string, v *viewer) {
	f, err := os.Open(path)
	if err != nil {
		fail(err)
	}
	opened, offset := mustStat(f), tailOffset(f, 10)
	_, err = f.Seek(offset, io.SeekStart)
	check(err)
	br := bufio.NewReader(f)
	var pending []byte
	for {
		chunk, err := br.ReadBytes('\n')
		pending = append(pending, chunk...)
		offset += int64(len(chunk))
		if err == nil {
			v.line(trimNewline(pending))
			pending = pending[:0]
			continue
		}
		if err != io.EOF {
			check(err)
		}
		v.flush()
		time.Sleep(250 * time.Millisecond)
		now, err := os.Stat(path)
		if err != nil {
			continue
		}
		if os.SameFile(opened, now) && now.Size() >= offset {
			continue
		}
		if len(pending) > 0 {
			v.line(pending)
			pending = pending[:0]
		}
		check(f.Close())
		f = reopen(path)
		opened, offset = mustStat(f), 0
		br.Reset(f)
	}
}

// reopen opens path once it exists again: rotation can remove it between
// the stat that saw it and the open.
func reopen(path string) *os.File {
	for {
		f, err := os.Open(path)
		if err == nil {
			return f
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// tailOffset is where the file's last n lines begin, read backwards from its
// end; a final newline does not open an empty line.
func tailOffset(f *os.File, n int) int64 {
	end := mustStat(f).Size()
	buf := make([]byte, 64*1024)
	seen := 0
	for pos := end; pos > 0; {
		size := min(int64(len(buf)), pos)
		pos -= size
		_, err := f.ReadAt(buf[:size], pos)
		check(err)
		for i := size - 1; i >= 0; i-- {
			if buf[i] != '\n' || pos+i == end-1 {
				continue
			}
			seen++
			if seen == n {
				return pos + i + 1
			}
		}
	}
	return 0
}

func mustStat(f *os.File) os.FileInfo {
	stat, err := f.Stat()
	check(err)
	return stat
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
