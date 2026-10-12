# go-utils

Offensive programming utilities for Go. Fail fast, fail loud, no error recovery. A confused program should scream. Includes Redis-style environment KV store, file/directory operations with panic-on-error semantics, persistent dedup filter, and TUI formatting helpers.

`go get github.com/adriangalilea/go-utils`

```go
import . "github.com/adriangalilea/go-utils" //nolint:staticcheck

// Offensive programming - fail loud
config := Must(LoadConfig("config.json"))
Assert(config.Port > 0, "invalid port")

// Logging: a message, then fields. Symbols at a terminal, JSON events elsewhere
Log.Success("transcode complete", F{"file": name})
theater := Log.Scope("theater")                  // scope ["theater"], obeys THEATER_LOG_LEVEL
theater.Error("scan failed", F{"err": err})      // ⨯ [theater] scan failed err=EOF
s := Log.Span("sync", F{"repo": repo})           // ○ sync repo=ui … ✓ sync 254ms
defer s.End(&err)

// Redis-style env vars
apiKey := KEV.Get("API_KEY")      // memory → os → .env
port := KEV.Int("PORT", 8080)
threshold := KEV.Float("ALERT_THRESHOLD", 0.95)
KEV.Set("DEBUG", "true")

// Namespaced access (bypass memory)
foo := KEV.Get("../.env:FOO")     // read from parent dir .env
KEV.Set("os:DEBUG", "true")       // write to OS env

// File ops that panic on error
data := File.Read("data.json")
File.Write("output.txt", processed)

// XDG Base Directories
XDG.State("notify")                    // ~/.local/state/notify
XDG.State("notify", "watchers.json")   // ~/.local/state/notify/watchers.json
XDG.Config("myapp")                    // ~/.config/myapp
Dir.Create(XDG.State("notify"))        // Ensure the directory exists

// Currency formatting with intelligent decimals
price := 0.037473  // ETH/BTC
Format.Currency.Auto(price, "BTC")  // "+0.037473 ₿" (green)
Format.Currency.USD(1234.56)        // "+$1,234.56" (green)
change := Currency.PercentageChange(100, 115)  // 15.0

// Byte counts, decimal SI like drives and `df -H` (the twin of @adriangalilea/utils bytes())
Bytes(850_000_000_000)  // "850.0 GB"
Bytes(4096)             // "4096 B"
Bytes(400 * GB)         // "400.0 GB" — a size stated in GB converts with GB

// Queue with automatic deduplication
q := NewQueue[string, Work](100)
results := q.Process(ctx, 5, func(ctx context.Context, url string, work Work) error {
    return processWork(url, work)
})
q.TryEnqueue("api.com", work1)  // Enqueued
q.TryEnqueue("api.com", work2)  // AlreadyQueued

// "What's new since last time?"
messages := fetchMessages()
newMessages := Unseen("messages", messages, func(m Message) string { return m.ID })

// Priority queue with fairness and built-in backoff
pq := NewPriorityQueue[string, Work](10, 100, 2)  // 2:1 ratio
defer pq.Stop()
results := pq.Process(ctx, 5, handler)
pq.TryEnqueue("critical", work, true)      // Priority queue
pq.TryEnqueue("normal", work, false)       // Normal queue
pq.TryEnqueue("retry", work, true, failures)  // With linear backoff (skip 'failures' times)
```

Part of the utils suite by Adrian Galilea: **go-utils**, **[ts-utils](https://github.com/adriangalilea/ts-utils)**, **[py-utils](https://github.com/adriangalilea/py-utils)**. The logging contract the three share, and the harness that proves they write the same event stream byte for byte, live in **[utils](https://github.com/adriangalilea/utils)**.

## Files

[**kev.go**](kev.go): Redis-style KV store for environment variables with namespace support (os:KEY, .env:KEY), memory caching, source fallback chains, pattern matching, and type conversions. Future: memory encryption and .kenv format.

[**offensive.go**](offensive.go): Offensive programming primitives. All panic with `*Panic` type instead of `os.Exit(1)`: full stack traces, testable via `recover()`, distinguishable from runtime errors at catch boundaries. Plus `SourcedError`, the one error type for what is not a bug: a boundary failure with its source, operation, status and context, logged with all of them.

```go
// Assert — invariant checking
// before:
if port <= 0 || port >= 65536 {
    log.Fatalf("invalid port: %d", port)
}

// after:
Assert(port > 0 && port < 65536, "invalid port:", port)

// Must — unwrap (value, error) pairs
// before:
tmpl, err := template.New("").Parse(`Hello {{.Name}}`)
if err != nil {
    log.Fatal(err)
}

// after:
tmpl := Must(template.New("").Parse(`Hello {{.Name}}`))

// Check — check error-only returns
// before:
err := os.WriteFile(path, data, 0644)
if err != nil {
    log.Fatal(err)
}

// after:
Check(os.WriteFile(path, data, 0644))

// Deref — read optional pointer fields (messy world boundary)
// before:
name := "unknown"
if cur.Location.Name != nil {
    name = *cur.Location.Name
}
fmt.Printf("weather in %s\n", name)

// after:
fmt.Printf("weather in %s\n", Deref(cur.Location.Name, "unknown"))

// Ptr — construct optional pointer fields (can't take address of literals in Go)
// before:
limit := 10
req := &SearchParams{Limit: &limit}

// after:
req := &SearchParams{Limit: Ptr(10)}
```

[**file.go**](file.go): File operations that panic on error — Read(), ReadString(), Write(), WriteString(), Open(), Create(), Exists(), Remove(), Copy(). Write is atomic (temp file + fsync + rename): a crash mid-write leaves the old content, never a truncated file.

[**dir.go**](dir.go): Directory operations that panic on error — Create(), Exists(), Remove(), List(), ListFull(), Copy(), Current(), Change(). Clean namespace, panics with `*Panic` via Check().

[**formatter.go**](formatter.go): Styled strings under the Format namespace for TUI views (Bubbletea) and CLI output: colours, Money(), Percent(), Number(), Bytes(), and Format.Currency for crypto/fiat amounts with sensible decimals.

[**logger.go**](logger.go): `Log`, the event stream of the [logging contract](https://github.com/adriangalilea/utils#the-logging-contract), the same one ts-utils and py-utils write, so a Go daemon, a TypeScript worker and a Python script can be read, merged and traced together. Every verb takes a message, then at most one `F` of fields:

```go
Log.Info("ready")
Log.Warn("retrying", F{"attempt": 2})
Log.Error("charge failed", F{"err": err})   // err: type, message, its Unwrap chain as cause
```

The verbs say what happened and each has a fixed level: `Error` and `Fail` (error), `Warn` and `WarnOnce` (warn, once per message and scope), `Info`, `Success`, `Wait`, `Ready`, `Step` (info), `Debug`, `Trace`. Passing two `F` panics.

`Log.Scope("api").Scope("auth")` is the subsystem: its events carry `scope: ["api","auth"]` and filter by `API_AUTH_LOG_LEVEL`, then `API_LOG_LEVEL`, then `LOG_LEVEL`, then `SetLogLevel`, then info (`silent < error < warn < info < debug < trace`, any case, read live through KEV so `.env` works).

A span is a piece of work, and a span is a logger: it has every verb, `Span` (a child), `Set` (fields learned while it runs) and `Traceparent()`. It ends with the function, failed when the function returns an error:

```go
func charge(amount int) (err error) {
    s := Log.Span("charge", F{"amount": amount})   // ○ charge amount=1200
    defer s.End(&err)                               // ✓ charge 254ms, or ⨯ with fields.err
    s.Warn("retrying", F{"attempt": 2})             // indented under the span
    return stripe(s)                                // pass the span on: there is no context.Context
}
```

A trace crosses processes explicitly: hand on `s.Traceparent()` in an HTTP `traceparent` header or a `TRACEPARENT` variable; `Log.Join(r.Header.Get("traceparent"))` continues it, and a process started with `TRACEPARENT` set continues it for its root spans. A span hidden by its level writes neither start nor end; what it holds belongs to the nearest span that is shown. A failure is never lost to that: a hidden span that fails writes one `fail` line in the nearest shown span, its fields with `err` and `dur_ms`.

Everything goes to stderr, stdout stays data. `LOG_FORMAT=json` writes one JSON event per line in the contract's key order, `human` writes symbols, colour, dim `key=value` fields, error stacks and indented spans; the default is human when stderr is a terminal and json otherwise. `LOG_TIME=1` opens human lines with a dim local `HH:MM:SS`. `LOG_SERVICE` names the program in every event (default: the executable's name). Each variable has a setter (`SetLogFormat`, `SetLogTime`, `SetLogLevel`, `SetLogService`, `SetLogShip`, `SetLogShipToken`) that is the program's default; the environment wins over it. An unknown value panics. Field values follow the contract's table ([fields.go](fields.go)): `time.Time` as the event time, `time.Duration` as milliseconds, `[]byte` as base64, structs through `encoding/json`, map keys sorted at every depth.

[**ship.go**](ship.go): with `LOG_SHIP` set, every event that passes its threshold is also POSTed to that URL as its json line, byte for byte, whatever stderr renders, so a daemon ships its stream with no code:

```sh
LOG_SHIP=https://events.untitled.garden/ingest LOG_SHIP_TOKEN=… my-daemon
```

```go
func main() {
    defer FlushLog()   // Go has no exit hook: send what waits before main returns
    ...
}
```

A batch is lines joined by `\n`, `Content-Type: text/plain;charset=utf-8`, at most 64 KB and 50 events, with `LOG_SHIP_TOKEN` as `Authorization: Bearer`. It leaves 2s after its first event, at 50 events, and when a span opened outside any span of this process ends (a root span, or a request joined from its caller: its whole trace leaves together). `FlushLog()` sends what waits and returns once no batch is in flight; call it before `main` returns or `os.Exit`. Shipping never blocks a write and never panics: a refused or lost batch is one `WarnOnce` under scope `ship` (no retry), an event over 64 KB stays on stderr and warns once. A malformed `LOG_SHIP` panics. The wire and the collector's answers are the contract's, in [utils](https://github.com/adriangalilea/utils#shipping).

[**currencies.go**](currencies.go): Currency namespace with intelligent decimal formatting, Unicode symbols (₿, Ξ, €, etc.), percentage calculations, and currency type detection. Optimized for crypto trading with BTC/ETH precision handling.

[**unseen.go**](unseen.go): "What's new since last time?" — filters a slice of structs to only the ones you haven't seen before. Remembers across runs. Idempotent — safe to re-run. State: `$XDG_STATE_HOME/unseen/{namespace}.json`.

[**xdg.go**](xdg.go): XDG Base Directory paths — reads env vars set by [xdg-dirs](https://github.com/adriangalilea/xdg-dirs), falls back to spec defaults. Variadic path segments for clean composition with Dir.Create().

[**net.go**](net.go): Network range math the stdlib's `net/netip` leaves out — Net.First(), Last(), Broadcast(), Size(), UsableSize() over netip.Prefix. For the IP types themselves use netip.Addr/netip.Prefix directly; they already make invalid states unrepresentable.

[**queue.go**](queue.go): Thread-safe work queue with automatic deduplication - Queue[K,V] ensures each key is queued at most once until completion. Perfect for API calls, background jobs, and event processing that must run exactly once. Features result channels, retry support, graceful drain, batch operations, and metrics hooks.

[**priority_queue.go**](priority_queue.go): Dual-queue system with fairness guarantees and built-in skip-based backoff - PriorityQueue[K,V] maintains two permanent queues where priority items never demote. Features linear backoff via skip counts (items with skipCount > 0 are decremented and re-enqueued). Configurable fairness ratio (e.g., 2:1) ensures normal queue isn't starved. Dispatcher starts on first Process() call, multiple Process() calls share same dispatcher. Perfect for connection retry logic, tiered service handling, critical infrastructure monitoring.

## CLI Tools

### logs

Reads the event stream any of the three libraries writes, from files or stdin. At a terminal it renders it as the logger does at one; in a pipe it writes one grep line per event, so `logs app.log | rg WARN` works as grepping a file did:

```
2026-10-12T12:20:39.123Z WARN  [payments] retrying attempt=2
```

A line that is not an event (a Go panic, `net/http`'s own error log) is printed as it is, never dropped, whatever the filters.

```bash
# Install
go install github.com/adriangalilea/go-utils/cmd/logs@latest

# Usage
logs app.log                     # render a file
my-daemon 2>&1 | logs            # render a live stream
logs -f app.log                  # follow from the last 10 lines, across rotation
logs --level warn app.log        # warn and error only
logs --scope api app.log         # [api] and [api auth], ...
logs --trace 4bf92f3577b34da6a3ce929d0e0e4736 app.log
```

Named `logs` because `log` is a zsh builtin and `/usr/bin/log` on macOS.

### go-check

Code quality checker with auto-fix and dead code detection.

```bash
# Install
go install github.com/adriangalilea/go-utils/cmd/go-check@latest

# Usage
go-check          # Run all checks (default)
go-check fix      # Auto-fix issues  
go-check dead     # Find dead code only
go-check internal # Find unused unexported functions
go-check --help   # Show help
```

Auto-installs `golangci-lint` and `deadcode` on first run.