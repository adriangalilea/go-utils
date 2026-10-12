# go-utils

## Offensive programming

- **Assert your assumptions** - Wrong world view = immediate crash
- **No error handling** - Let it crash, the stack trace is the documentation
- **Single source of truth**

## No fmt.Sprintf

Format strings hide data flow. 

```go
// BAD
Log.Info(fmt.Sprintf("Found %d nodes", count))

// GOOD: the message says what happened, the fields say with what
Log.Info("found nodes", F{"count": count})
```

Log messages are fixed text; every value goes in `F`. Use `String()` for conversion in Panic and Assert messages, not `fmt.Sprintf()` for formatting.

## Logging contract

`logger.go`, `fields.go`, `ship.go` and `internal/stream` write the event stream specified in `~/Developer/utils` (README, schema, conformance). Change them only to match that contract, and prove it with `LOCAL=~/Developer ~/Developer/utils/conformance/run.sh` (the `go` line must PASS) plus `go test ./...`.

Library code never prints by itself (`fmt.Print*`, the `log` package, `println`, `os.Stdout`/`os.Stderr` used directly): every line goes through `Log.Scope(...)` so the stream stays all events. `TestNoRawWrites` (`rawwrite_test.go`) refuses new ones; a deliberate stdout data writer goes in its allowlist with a reason.

## Available Utilities

@README.md
