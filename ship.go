package utils

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Shipping: with LOG_SHIP set (or SetLogShip), every event that passes its
// threshold is also POSTed there as the stream's json line, whatever stderr
// renders. A batch is lines joined by "\n", Content-Type
// text/plain;charset=utf-8, at most 64 KB and 50 events, with
// LOG_SHIP_TOKEN (or SetLogShipToken) as its bearer. It leaves 2s after its
// first event, at 50 events, and when a span opened outside any span of this
// process ends (a root, or one joined from its caller), so a request's trace
// leaves together. Go has no hook before a process exits: a program that
// ships calls FlushLog before it ends. Shipping never blocks a write and
// never panics: a refused or lost batch is one WarnOnce, scope ["ship"], and
// an event over 64 KB stays on stderr and warns once. A malformed LOG_SHIP is
// a configuration error and panics, as every variable does.
const (
	shipMaxBytes  = 64 * 1024 // navigator.sendBeacon's ceiling, the contract's for every writer
	shipMaxEvents = 50
	shipAfter     = 2 * time.Second
	// A collector that hangs costs FlushLog this much, then one warning.
	shipTimeout = 10 * time.Second
)

var (
	shipLog    = Log.Scope("ship")
	shipClient = &http.Client{Timeout: shipTimeout}
)

type shipBatch struct {
	target string
	body   []byte // the lines joined by "\n"
	lines  int
	timer  *time.Timer
}

// shipping holds the batch filling up and the count of batches being POSTed.
// idle is signalled each time one finishes, for FlushLog.
var shipping struct {
	sync.Mutex
	batch    *shipBatch
	inflight int
}

var shipIdle = sync.NewCond(&shipping.Mutex)

// SetLogShip is where events are also POSTed, an http(s) URL, when LOG_SHIP
// is unset.
func SetLogShip(target string) {
	href := parseShip("SetLogShip", target)
	settings.Lock()
	defer settings.Unlock()
	settings.ship = href
}

// SetLogShipToken is the bearer sent with each batch when LOG_SHIP_TOKEN is
// unset.
func SetLogShipToken(token string) {
	Assert(token != "", "SetLogShipToken wants a non-empty token")
	settings.Lock()
	defer settings.Unlock()
	settings.shipToken = token
}

func parseShip(name, raw string) string {
	u, err := url.Parse(raw)
	Assert(err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "", "unknown", name+":", raw, "(want an http(s) URL)")
	return u.String()
}

// shipTarget is LOG_SHIP, read live, else SetLogShip's; "" ships nothing.
func shipTarget() string {
	if raw := KEV.get("LOG_SHIP", false, ""); raw != "" {
		return parseShip("LOG_SHIP", raw)
	}
	settings.Lock()
	defer settings.Unlock()
	return settings.ship
}

func shipToken() string {
	if token := KEV.get("LOG_SHIP_TOKEN", false, ""); token != "" {
		return token
	}
	settings.Lock()
	defer settings.Unlock()
	return settings.shipToken
}

// ship adds one json line to the waiting batch, first sending the batch it
// would not fit in. It is called under out's lock, so batches hold lines in
// the order stderr has them. False: the line alone is over the ceiling and is
// not shipped.
func ship(target string, line []byte) bool {
	if len(line) > shipMaxBytes {
		return false
	}
	shipping.Lock()
	defer shipping.Unlock()
	if b := shipping.batch; b != nil && (b.target != target || len(b.body)+1+len(line) > shipMaxBytes) {
		sendLocked()
	}
	b := shipping.batch
	if b == nil {
		b = &shipBatch{target: target}
		// The timer sends this batch only: one sent earlier is already gone.
		b.timer = time.AfterFunc(shipAfter, func() {
			shipping.Lock()
			defer shipping.Unlock()
			if shipping.batch == b {
				sendLocked()
			}
		})
		shipping.batch = b
	} else {
		b.body = append(b.body, '\n')
	}
	b.body = append(b.body, line...)
	b.lines++
	if b.lines >= shipMaxEvents {
		sendLocked()
	}
	return true
}

// send POSTs the waiting batch, if any, without waiting for the answer.
func send() {
	shipping.Lock()
	defer shipping.Unlock()
	sendLocked()
}

func sendLocked() {
	b := shipping.batch
	if b == nil {
		return
	}
	shipping.batch = nil
	b.timer.Stop()
	shipping.inflight++
	go post(b)
}

// post sends one batch. Any answer but 204 is a refused batch.
func post(b *shipBatch) {
	defer func() {
		shipping.Lock()
		shipping.inflight--
		shipIdle.Broadcast()
		shipping.Unlock()
	}()
	req, err := http.NewRequest(http.MethodPost, b.target, bytes.NewReader(b.body))
	Assert(err == nil, "a parsed LOG_SHIP makes no request:", err)
	req.Header.Set("Content-Type", "text/plain;charset=utf-8")
	if token := shipToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := shipClient.Do(req)
	if err != nil {
		shipLog.WarnOnce("log batch not delivered", F{"err": err})
		return
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent {
		return
	}
	answer, _ := io.ReadAll(io.LimitReader(res.Body, 1000))
	shipLog.WarnOnce("log batch refused", F{"status": res.StatusCode, "answer": string(answer)})
}

// FlushLog sends the waiting batch and returns once no batch is in flight.
// Nothing runs after a Go program returns from main or calls os.Exit, so a
// program that ships calls it last: defer FlushLog() at the top of main, or
// before os.Exit. A refused batch's warning is an event too: it ships before
// FlushLog returns.
func FlushLog() {
	shipping.Lock()
	defer shipping.Unlock()
	sendLocked()
	for shipping.inflight > 0 {
		shipIdle.Wait()
		sendLocked()
	}
}
