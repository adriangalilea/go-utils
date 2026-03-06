// XDG Base Directory paths.
//
// Reads env vars set by xdg-dirs (https://github.com/adriangalilea/xdg-dirs),
// falls back to spec defaults (https://specifications.freedesktop.org/basedir-spec).
//
// Usage:
//
//	XDG.State("notify")                    // ~/.local/state/notify
//	XDG.State("notify", "watchers.json")   // ~/.local/state/notify/watchers.json
//	XDG.Config("myapp")                    // ~/.config/myapp
//	XDG.Data("myapp")                      // ~/.local/share/myapp
//	XDG.Cache("myapp")                     // ~/.cache/myapp
//	XDG.Runtime("myapp")                   // $XDG_RUNTIME_DIR/myapp
//
//	// Ensure the directory exists before writing
//	Dir.Create(XDG.State("notify"))
package utils

import (
	"os"
	"path/filepath"
)

type xdgOps struct{}

// XDG provides XDG Base Directory paths
var XDG = xdgOps{}

func xdgDir(envKey, fallback string, segments ...string) string {
	base := os.Getenv(envKey)
	if base == "" {
		base = fallback
	}
	return filepath.Join(append([]string{base}, segments...)...)
}

func (xdgOps) Config(segments ...string) string {
	return xdgDir("XDG_CONFIG_HOME", filepath.Join(Must(os.UserHomeDir()), ".config"), segments...)
}

func (xdgOps) Data(segments ...string) string {
	return xdgDir("XDG_DATA_HOME", filepath.Join(Must(os.UserHomeDir()), ".local", "share"), segments...)
}

func (xdgOps) State(segments ...string) string {
	return xdgDir("XDG_STATE_HOME", filepath.Join(Must(os.UserHomeDir()), ".local", "state"), segments...)
}

func (xdgOps) Cache(segments ...string) string {
	return xdgDir("XDG_CACHE_HOME", filepath.Join(Must(os.UserHomeDir()), ".cache"), segments...)
}

func (xdgOps) Runtime(segments ...string) string {
	return xdgDir("XDG_RUNTIME_DIR", filepath.Join(Must(os.UserHomeDir()), ".local", "run"), segments...)
}
