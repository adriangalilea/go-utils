// "What's new since last time?" — filters a slice of structs to only
// the ones you haven't seen before. Remembers across runs.
//
//	messages := fetchMessages()
//	newMessages := Unseen("messages", messages, func(m Message) string { return m.ID })
//
//	1st run:
//	  messages    = [{ID: "1", From: "alice", Text: "hi"}]
//	  newMessages = [{ID: "1", From: "alice", Text: "hi"}]
//
//	2nd run, no new message:
//	  newMessages = []
//
//	3rd run, bob replied:
//	  messages    = [{ID: "1", ...}, {ID: "2", From: "bob", Text: "hey"}]
//	  newMessages = [{ID: "2", From: "bob", Text: "hey"}]
//
// Saves state to: $XDG_STATE_HOME/unseen/{namespace}.json
package utils

import (
	"encoding/json"
	"path/filepath"
)

var unseenDir = XDG.State("unseen")

// Unseen filters items to only those not seen before for the given namespace.
// key extracts a unique string identifier from each item.
func Unseen[T any](namespace string, items []T, key func(T) string) []T {
	storePath := filepath.Join(unseenDir, namespace+".json")
	Dir.Create(filepath.Dir(storePath))

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
