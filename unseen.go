// Persistent dedup filter — "what's new since last time?"
//
// Makes any script idempotent. Run it once or a thousand times,
// you only process each item once. Any scheduling works:
// manual, cron, a loop, whatever.
//
//	1st run: 5 orders exist  → returns 5
//	2nd run: same 5 orders   → returns 0
//	3rd run: 7 orders exist  → returns 2
//
// Usage:
//
//	fresh := Unseen("orders", orders, func(o Order) string { return o.ID })
//	for _, o := range fresh {
//	    notify(o.Summary)
//	}
//
// State persists at ~/.local/state/unseen/{namespace}.json
package utils

import (
	"encoding/json"
	"path/filepath"
)

var unseenDir = XDG.State("unseen")

// Unseen filters items to only those not seen before for the given namespace.
// key extracts a unique string identifier from each item.
func Unseen[T any](namespace string, items []T, key func(T) string) []T {
	Dir.Create(unseenDir)
	storePath := filepath.Join(unseenDir, namespace+".json")

	var keys []string
	if File.Exists(storePath) {
		Check(json.Unmarshal(File.Read(storePath), &keys))
	}

	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		seen[k] = true
	}

	var result []T
	for _, item := range items {
		k := key(item)
		if !seen[k] {
			seen[k] = true
			result = append(result, item)
		}
	}

	allKeys := make([]string, 0, len(seen))
	for k := range seen {
		allKeys = append(allKeys, k)
	}
	File.Write(storePath, Must(json.Marshal(allKeys)))
	return result
}
