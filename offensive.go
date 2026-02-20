package utils

import (
	"fmt"
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

// Assert panics if condition is false.
// Use for validating preconditions and invariants.
//
// Examples:
//
//	func SendPacket(data []byte, port int) {
//	    Assert(len(data) > 0, "empty packet")
//	    Assert(port > 0 && port < 65536, "invalid port:", port)
//	}
func Assert(condition bool, msg ...interface{}) {
	if !condition {
		message := "assertion failed"
		if len(msg) > 0 {
			message = fmt.Sprint(msg...)
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
			message = strings.Join(messages, " ")
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
