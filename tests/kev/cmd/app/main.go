package main

import (
	utils "github.com/adriangalilea/go-utils"
)

func main() {
	utils.Log.Info("running from cmd/app", utils.F{"sources": utils.KEV.Source.List()})

	// Should find project root
	utils.Log.Info("read", utils.F{"key": "PROJECT_ROOT", "value": utils.KEV.Get("PROJECT_ROOT")})

	// Should find cmd level
	utils.Log.Info("read", utils.F{"key": "CMD_LEVEL", "value": utils.KEV.Get("CMD_LEVEL")})

	// Test precedence - local .env should win
	utils.Log.Info("read", utils.F{"key": "SHARED_KEY", "value": utils.KEV.Get("SHARED_KEY")})

	// Show where they came from
	for _, key := range []string{"PROJECT_ROOT", "CMD_LEVEL", "SHARED_KEY"} {
		utils.Log.Info("source", utils.F{"key": key, "from": utils.KEV.SourceOf(key)})
	}
}
