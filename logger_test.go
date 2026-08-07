package utils

import (
	"testing"
	"time"
)

// The record line is the shared grep surface of go-utils, ts-utils and
// py-utils. This golden line must match theirs byte for byte.
func TestRecordLineGolden(t *testing.T) {
	ts := time.Date(2026, 8, 7, 12, 34, 56, 0, time.UTC)

	got := recordLine(ts, "WARN", "theater", "scan failed: EOF")
	want := "2026-08-07T12:34:56Z WARN  [theater] scan failed: EOF"
	if got != want {
		t.Fatalf("scoped record line\n got: %q\nwant: %q", got, want)
	}

	got = recordLine(ts, "ERROR", "", "boom")
	want = "2026-08-07T12:34:56Z ERROR boom"
	if got != want {
		t.Fatalf("unscoped record line\n got: %q\nwant: %q", got, want)
	}

	got = recordLine(time.Date(2026, 8, 7, 14, 34, 56, 0, time.FixedZone("CEST", 2*3600)), "INFO", "api auth", "token refreshed")
	want = "2026-08-07T12:34:56Z INFO  [api auth] token refreshed"
	if got != want {
		t.Fatalf("local time must render as UTC\n got: %q\nwant: %q", got, want)
	}
}

func TestEnvKey(t *testing.T) {
	cases := map[string]string{
		"theater":  "THEATER",
		"api-v2":   "API_V2",
		"go check": "GO_CHECK",
	}
	for in, want := range cases {
		if got := envKey(in); got != want {
			t.Fatalf("envKey(%q) = %q, want %q", in, got, want)
		}
	}
}
