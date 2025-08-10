package utils

import (
	"os"
	"strings"
)

// OFFENSIVE PROGRAMMING PRIMITIVES
//
// "A confused program SHOULD scream" - John Carmack
//
// These utilities are the ANTITHESIS of defensive programming.
// 
// Defensive programming: try/catch, error recovery, graceful degradation, silent failures
// Offensive programming: FAIL LOUD, FAIL FAST, NO RECOVERY, CRASH EARLY
//
// We don't handle errors - we make them catastrophic.
// We don't recover - we crash.
// We don't validate and continue - we assert and panic.
//
// This approach makes bugs IMPOSSIBLE to ignore:
// - Wrong assumptions? CRASH
// - Invalid state? CRASH  
// - "Impossible" error? CRASH
//
// The only acceptable response to confusion is to scream and die.

// ERROR HANDLING HIERARCHY
// 
// 1. Assert - For validating assumptions/invariants -> panics (logic error)
// 2. Must - For operations that shouldn't fail -> panics (programmer error)  
// 3. Check - For expected runtime errors (user/system errors) -> exits cleanly

// Assert exits with error if condition is false.
// Use for validating preconditions and invariants.
//
// Examples:
//   func SendPacket(data []byte, port int) {
//       Assert(len(data) > 0, "empty packet")
//       Assert(port > 0 && port < 65536, "invalid port:", port)
//       // Now safe to proceed
//   }
func Assert(condition bool, msg ...interface{}) {
	if !condition {
		if len(msg) > 0 {
			Log.Error(msg...)
		} else {
			Log.Error("assertion failed")
		}
		os.Exit(1)
	}
}

// Must unwraps (value, error) pairs and exits if error is not nil.
// Use for operations that should never fail in correct code.
//
// Examples:
//   tmpl := Must(template.New("").Parse(`Hello {{.Name}}`))  // Static template
//   regex := Must(regexp.Compile(`^\d+$`))                    // Static regex
//   
// If these fail, it's a bug in the code, not a user error.
func Must[T any](val T, err error) T {
	if err != nil {
		Log.Error(err)
		os.Exit(1)
	}
	return val
}

// Check exits cleanly with formatted error message if err is not nil.
// Use for expected errors: file not found, network issues, permissions, etc.
//
// Examples:
//   data, err := os.ReadFile(userFile)
//   Check(err)  // ✗ open config.json: no such file or directory -> exits
//   
//   file, err := os.Open(path)
//   Check(err, "Cannot open config")  // ✗ Cannot open config -> exits
func Check(err error, messages ...string) {
	if err != nil {
		if len(messages) > 0 {
			// Use custom message instead of error
			Log.Error(strings.Join(messages, " "))
		} else {
			// Use error's own message
			Log.Error(err)
		}
		os.Exit(1)
	}
}