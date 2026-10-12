package utils

import (
	"strconv"
	"strings"
)

// OFFENSIVE PROGRAMMING PRIMITIVES
//
// "A confused program SHOULD scream" - John Carmack
//
// panic(), always. An uncaught panic crashes the process with a full stack trace.
// Zero external dependencies. Testable via recover().
//
// Five primitives:
//   Assert(cond, ...msg)       - invariant checking
//   Must[T](val, err) T        - unwrap (value, error) pairs
//   Check(err, ...msg)         - check error-only returns
//   Deref[T](ptr, fallback) T  - unwrap pointer or use fallback (messy world boundary)
//   Ptr[T](val) *T             - address-of literal (construct optional fields)
//
// And one error type for what is not a bug: SourcedError, a boundary failure
// with its source, operation, status and context.
//
// All panic with Panic type — distinguishes bugs from runtime errors:
//
//   defer func() {
//       if r := recover(); r != nil {
//           if _, ok := r.(*Panic); ok {
//               // bug in the program, re-panic
//               panic(r)
//           }
//           // runtime error, handle gracefully
//       }
//   }()
//
//   // In tests:
//   assert.PanicsWithValue(t, &Panic{...}, func() { Assert(false, "boom") })

// Panic is the error type for offensive programming failures.
// Distinguishes bugs from runtime errors at recover() boundaries.
type Panic struct {
	Message string
}

func (p *Panic) Error() string {
	return p.Message
}

// SourcedError is the world failing, not the program: a boundary with the
// messy world (an HTTP API, a database, a process) that refused. It carries
// which system, which operation, the status when there is one (0: none), the
// underlying error and the context needed to reconstruct the failure without
// a debugger. Panics are bugs; SourcedErrors are handled. The twin of
// SourcedError in ts-utils and py-utils, and logged the same way: as a field
// value it writes source, operation, status and context after its message.
//
//	return &SourcedError{Source: "stripe", Operation: "charge", Message: "card declined",
//	    Status: 402, Cause: err, Context: map[string]any{"customer": id}}
type SourcedError struct {
	Source    string
	Operation string
	Message   string
	Status    int
	Cause     error
	Context   map[string]any
}

// Error reads "[stripe:charge status=402] card declined".
func (e *SourcedError) Error() string {
	head := e.Source + ":" + e.Operation
	if e.Status != 0 {
		head += " status=" + strconv.Itoa(e.Status)
	}
	return "[" + head + "] " + e.Message
}

func (e *SourcedError) Unwrap() error { return e.Cause }

// Assert panics if condition is false.
// Use for validating preconditions and invariants.
//
// Examples:
//
//	func SendPacket(data []byte, port int) {
//	    Assert(len(data) > 0, "empty packet")
//	    Assert(port > 0 && port < 65536, "invalid port:", port)
//	}
func Assert(condition bool, msg ...any) {
	if !condition {
		message := "assertion failed"
		if len(msg) > 0 {
			message = String(msg...)
		}
		panic(&Panic{Message: message})
	}
}

// Must unwraps (value, error) pairs and panics if error is not nil.
// Use for operations that should never fail in correct code.
//
// Examples:
//
//	tmpl := Must(template.New("").Parse(`Hello {{.Name}}`))
//	regex := Must(regexp.Compile(`^\d+$`))
//	data := Must(os.ReadFile("config.json"))
func Must[T any](val T, err error) T {
	if err != nil {
		panic(&Panic{Message: err.Error()})
	}
	return val
}

// Check panics if err is not nil.
// Use for error-only returns: os.WriteFile, os.Remove, file.Close, etc.
//
// Must() is for (value, error) pairs. Check() is for bare error returns.
//
// Examples:
//
//	err := os.WriteFile(path, data, 0644)
//	Check(err)
//
//	Check(file.Close())
//	Check(err, "Cannot open config")
func Check(err error, messages ...string) {
	if err != nil {
		message := err.Error()
		if len(messages) > 0 {
			message = strings.Join(messages, " ") + ": " + err.Error()
		}
		panic(&Panic{Message: message})
	}
}

// Deref returns the value behind a pointer, or fallback if nil.
// For dealing with optional fields from JSON, API responses, etc.
//
// Examples:
//
//	temp := Deref(weather.Temperature, 0.0)
//	name := Deref(user.DisplayName, "anonymous")
func Deref[T any](ptr *T, fallback T) T {
	if ptr != nil {
		return *ptr
	}
	return fallback
}

// Ptr returns a pointer to the given value.
// Go can't take the address of a literal — this fixes that.
//
// Examples:
//
//	req := &Request{Limit: Ptr(10), Name: Ptr("test")}
func Ptr[T any](val T) *T {
	return &val
}
