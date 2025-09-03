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

// GOOD - Log formats for you
Log.Info("Found", count, "nodes")
```

Use `String()` for conversion, not `fmt.Sprintf()` for formatting.

## Available Utilities

@README.md
