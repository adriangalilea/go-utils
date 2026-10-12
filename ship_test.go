package utils

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adriangalilea/go-utils/internal/stream"
)

type received struct {
	auth, contentType, body string
}

// collector records every batch POSTed to it and answers with what answer holds.
type collector struct {
	sync.Mutex
	batches []received
	status  int
	answer  string
}

func (c *collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.Lock()
	defer c.Unlock()
	if r.Method != http.MethodPost {
		panic("ship POSTs, got " + r.Method)
	}
	c.batches = append(c.batches, received{r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)})
	w.WriteHeader(c.status)
	_, _ = io.WriteString(w, c.answer)
}

func (c *collector) take() []received {
	c.Lock()
	defer c.Unlock()
	taken := c.batches
	c.batches = nil
	return taken
}

func (c *collector) reply(status int, answer string) {
	c.Lock()
	defer c.Unlock()
	c.status, c.answer = status, answer
}

func bodies(rs []received) []string {
	var b []string
	for _, r := range rs {
		b = append(b, r.body)
	}
	return b
}

// taken is what the logger wrote to stderr since the last call, as the lines
// a batch joins.
func taken() string {
	out.Lock()
	defer out.Unlock()
	buf := out.w.(*bytes.Buffer)
	s := strings.TrimSuffix(buf.String(), "\n")
	buf.Reset()
	return s
}

func panics(fn func()) (did bool) {
	defer func() { _, did = recover().(*Panic) }()
	fn()
	return false
}

// TestShip proves the LOG_SHIP sink: a batch is the stderr lines byte for
// byte, on the contract's wire; each trigger sends (50 events, a top-level
// span's end, 2s, FlushLog) and a nested span's end does not; 64 KB is the
// ceiling; only what passes the threshold ships; a refused or lost batch is
// one warning, never a panic; a bad configuration panics.
func TestShip(t *testing.T) {
	c := &collector{status: http.StatusNoContent}
	server := httptest.NewServer(c)
	defer server.Close()
	url := server.URL + "/ingest"
	settle := func() { time.Sleep(300 * time.Millisecond) }

	capture(t, "json", func() {
		// Off by default: nothing ships.
		Log.Info("not shipped")
		FlushLog()
		taken()
		if len(c.take()) != 0 {
			t.Fatal("no LOG_SHIP, no batch")
		}

		// The wire: stderr's bytes, one POST, text/plain, the bearer.
		SetLogShip(url)
		SetLogShipToken("tok-1")
		Log.Info("ready", F{"port": 1})
		Log.Warn("retrying", F{"attempt": 2, "nested": F{"b": 1, "a": []any{true, nil}}})
		FlushLog()
		b := c.take()
		if len(b) != 1 || b[0].body != taken() {
			t.Fatalf("one flush, one batch of the written lines: %+v", b)
		}
		if b[0].contentType != "text/plain;charset=utf-8" || b[0].auth != "Bearer tok-1" {
			t.Fatalf("content type and bearer: %+v", b[0])
		}
		// The environment wins over code, read live.
		KEV.Set("LOG_SHIP_TOKEN", "tok-env")
		Log.Info("env token")
		FlushLog()
		if b := c.take(); b[0].auth != "Bearer tok-env" {
			t.Fatalf("LOG_SHIP_TOKEN overrides SetLogShipToken: %q", b[0].auth)
		}
		KEV.Set("LOG_SHIP_TOKEN", "")
		taken()

		// The threshold: what is not written is not shipped.
		Log.Debug("hidden")
		Log.Info("shown")
		FlushLog()
		if got := strings.Join(bodies(c.take()), "\n"); got != taken() || strings.Count(got, "\n") != 0 {
			t.Fatalf("only events past the threshold ship: %s", got)
		}

		// Human on stderr, json on the wire.
		SetLogFormat("human")
		Log.Info("for a person", F{"k": "v"})
		FlushLog()
		shown := taken()
		b = c.take()
		e, ok := stream.Parse([]byte(b[0].body))
		if strings.HasPrefix(shown, "{") || !ok || e.Msg != "for a person" {
			t.Fatalf("human on stderr %q, json shipped %q", shown, b[0].body)
		}
		SetLogFormat("json")

		// 50 events send at once, without waiting for the timer.
		for i := range 50 {
			Log.Info("tick", F{"i": i})
		}
		settle()
		b = c.take()
		if len(b) != 1 || strings.Count(b[0].body, "\n") != 49 || b[0].body != taken() {
			t.Fatalf("50 events: one batch of 50 lines now, got %d batches", len(b))
		}

		// A top-level span's end sends its trace; a nested one's does not.
		Log.Info("before")
		root := Log.Span("request")
		db := root.Span("db")
		db.Info("query")
		db.End(nil)
		settle()
		if len(c.take()) != 0 {
			t.Fatal("a nested span's end does not send")
		}
		root.End(nil)
		settle()
		b = c.take()
		if len(b) != 1 || strings.Count(b[0].body, "\n") != 5 || b[0].body != taken() {
			t.Fatalf("a root span's end sends line, request, db, query, db end, request end: %+v", b)
		}
		// A request joined from its caller is top-level here: its end sends too.
		vote := Log.Join(remote).Span("POST /vote")
		vote.Info("voted")
		vote.End(nil)
		settle()
		if b := c.take(); len(b) != 1 || b[0].body != taken() {
			t.Fatal("a joined top-level span's end sends")
		}
		// A hidden root span still sends what it holds.
		KEV.Set("QUIET_LOG_LEVEL", "warn")
		hidden := Log.Scope("quiet").Span("hidden")
		hidden.Warn("kept")
		hidden.End(nil)
		KEV.Set("QUIET_LOG_LEVEL", "")
		settle()
		if got := strings.Join(bodies(c.take()), "\n"); strings.Count(got, "\n") != 0 || got != taken() {
			t.Fatalf("a hidden root span's end sends its one shown line: %q", got)
		}

		// 2s after the first event, with no other trigger.
		Log.Info("alone")
		time.Sleep(time.Second)
		if len(c.take()) != 0 {
			t.Fatal("not before 2s")
		}
		time.Sleep(1500 * time.Millisecond)
		if got := strings.Join(bodies(c.take()), "\n"); got != taken() {
			t.Fatalf("the timer sends the waiting batch: %q", got)
		}

		// 64 KB per batch, never more.
		big := strings.Repeat("x", 20_000)
		for i := range 7 {
			Log.Info("chunk", F{"big": big, "i": i})
		}
		FlushLog()
		got := bodies(c.take())
		if len(got) != 3 {
			t.Fatalf("7 × 20 KB: three batches, got %d", len(got))
		}
		// Three batches in flight at once may arrive in any order: each is a
		// run of the written lines, and together they are all of them.
		written, size := taken(), -1
		for _, body := range got {
			if len(body) > 64*1024 {
				t.Fatalf("a batch is at most 64 KB, got %d", len(body))
			}
			if !strings.Contains(written+"\n", body+"\n") {
				t.Fatal("a batch is a run of the written lines, in order")
			}
			size += len(body) + 1
		}
		if size != len(written) {
			t.Fatal("the batches are all the written lines, once")
		}
		// One event over the ceiling stays on stderr and warns once.
		Log.Info("huge", F{"blob": strings.Repeat("y", 70_000)})
		Log.Info("huge again", F{"blob": strings.Repeat("y", 70_000)})
		FlushLog()
		got = bodies(c.take())
		e, ok = stream.Parse([]byte(got[0]))
		if len(got) != 1 || !ok || e.Msg != "event too large to ship" || e.Verb.Level() != stream.LevelWarn || strings.Join(e.Scope, ",") != "ship" {
			t.Fatalf("an event over 64 KB is not shipped; one warning, scope ship, is: %q", got)
		}
		if n := strings.Count(taken(), "\n") + 1; n != 3 {
			t.Fatalf("both huge events and the warning are written, got %d lines", n)
		}

		// A refused batch: one warning, shipped with the next batch.
		c.reply(http.StatusBadRequest, `{"accepted":0,"rejected":[{"line":1,"why":"x"}]}`)
		Log.Info("refused")
		Log.Info("refused again")
		FlushLog()
		c.reply(http.StatusNoContent, "")
		b = c.take()
		if len(b) != 2 {
			t.Fatalf("the batch and the warning, got %d", len(b))
		}
		e, ok = stream.Parse([]byte(b[1].body))
		if !ok || e.Msg != "log batch refused" || fieldsJSON(e) != `{"answer":"{\"accepted\":0,\"rejected\":[{\"line\":1,\"why\":\"x\"}]}","status":400}` {
			t.Fatalf("refusal warned with status and answer: %s", b[1].body)
		}
		c.reply(http.StatusBadRequest, "")
		Log.Info("refused once more")
		FlushLog()
		c.reply(http.StatusNoContent, "")
		if len(c.take()) != 1 {
			t.Fatal("a refusal warns once")
		}
		taken()

		// A lost batch: one warning with the error.
		dead, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		deadURL := "http://" + dead.Addr().String() + "/ingest"
		dead.Close()
		SetLogShip(deadURL)
		Log.Info("into the void")
		FlushLog()
		SetLogShip(url)
		lines := strings.Split(taken(), "\n")
		e, ok = stream.Parse([]byte(lines[len(lines)-1]))
		if len(lines) != 2 || !ok || e.Msg != "log batch not delivered" || len(e.Fields) != 1 || e.Fields[0].Key != "err" {
			t.Fatalf("a lost batch is one warning with the error: %q", lines)
		}
		FlushLog()
		c.take()
		taken()

		// A confused configuration panics.
		if !panics(func() { SetLogShip("ftp://x/y") }) || !panics(func() { SetLogShip("not a url") }) {
			t.Fatal("a non-http(s) URL panics")
		}
		if !panics(func() { SetLogShipToken("") }) {
			t.Fatal("an empty token panics")
		}
		KEV.Set("LOG_SHIP", "nope")
		if !panics(func() { Log.Info("x") }) {
			t.Fatal("a bad LOG_SHIP panics on the next event")
		}
		KEV.Set("LOG_SHIP", "")
	})
}
