# go-utils

Offensive programming utilities for Go. Fail fast, fail loud, no error recovery. A confused program should scream. Includes Redis-style environment KV store, file/directory operations with panic-on-error semantics, and TUI formatting helpers.

`go get github.com/adriangalilea/go-utils`

```go
import . "github.com/adriangalilea/go-utils"

// Offensive programming - fail loud
config := Must(LoadConfig("config.json"))
Assert(config.Port > 0, "invalid port")

// Redis-style env vars
apiKey := KEV.Get("API_KEY")      // memory → os → .env
port := KEV.Int("PORT", 8080)
KEV.Set("DEBUG", "true")

// Namespaced access (bypass memory)
foo := KEV.Get("../.env:FOO")     // read from parent dir .env
KEV.Set("os:DEBUG", "true")       // write to OS env

// File ops that panic on error
data := File.Read("data.json")
File.Write("output.txt", processed)

// Currency formatting with intelligent decimals
price := 0.037473  // ETH/BTC
Format.Currency.Auto(price, "BTC")  // "0.037473 ₿"
Format.Currency.USD(1234.56)        // "+$1,234.56" (green)
change := Currency.PercentageChange(100, 115)  // 15.0

// Queue with automatic deduplication
q := NewQueue[string, Work](100)
results := q.Process(ctx, 5, func(ctx context.Context, url string, work Work) error {
    return processWork(url, work)
})
q.TryEnqueue("api.com", work1)  // true
q.TryEnqueue("api.com", work2)  // false - already queued
```

Part of the utils suite by Adrian Galilea. Planned: **go-utils** (available), **ts-utils** (coming), **py-utils** (coming).

## Files

[**kev.go**](kev.go): Redis-style KV store for environment variables with namespace support (os:KEY, .env:KEY), memory caching, source fallback chains, pattern matching, and type conversions. Future: memory encryption and .kenv format.

[**offensive.go**](offensive.go): Core offensive programming primitives - Assert() for invariants, Must() for operations that shouldn't fail, Check() for expected runtime errors. The antithesis of defensive programming.

[**file.go**](file.go): File operations that exit on error - Read(), Write(), Open(), Create(), Exists(), Remove(), Copy(). No error returns, just results or death.

[**dir.go**](dir.go): Directory operations that exit on error - Create(), Exists(), Remove(), List(), ListFull(), Copy(), Current(), Change(). Clean namespace, no error handling.

[**formatter.go**](formatter.go): String formatting utilities under Format namespace - Error(), Warn(), Info(), Wait(), Ready(), Event(), Trace() return formatted strings for TUI use (Bubbletea views). Includes Format.Currency for intelligent crypto/fiat formatting.

[**logger.go**](logger.go): Log namespace with level filtering via KEV.Get("LOG_LEVEL") - Error(), Warn(), Info(), Event(), Wait(), Ready(), Debug(), Trace(). Includes WarnOnce() for stateful warning deduplication.

[**currencies.go**](currencies.go): Currency namespace with intelligent decimal formatting, Unicode symbols (₿, Ξ, €, etc.), percentage calculations, and currency type detection. Optimized for crypto trading with BTC/ETH precision handling.

[**ip.go**](ip.go): Strong IPv4/IPv6 types that make invalid states unrepresentable - IPv4 (4 bytes), IPv6 (16 bytes), and IP discriminated union. Parse once at boundaries, guaranteed valid internally. No defensive checks needed after construction.

[**queue.go**](queue.go): Thread-safe work queue with automatic deduplication - Queue[K,V] ensures each key is queued at most once until completion. Perfect for API calls, background jobs, and event processing that must run exactly once. Features result channels, retry support, graceful drain, batch operations, and metrics hooks.