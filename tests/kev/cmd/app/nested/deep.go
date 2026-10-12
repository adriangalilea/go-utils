package main

import (
	utils "github.com/adriangalilea/go-utils"
)

func main() {
	utils.Log.Info("running from cmd/app/nested (deep)", utils.F{"sources": utils.KEV.Source.List()})

	// Should still find project root
	utils.Log.Info("read", utils.F{"key": "PROJECT_ROOT", "value": utils.KEV.Get("PROJECT_ROOT")})

	// Should NOT find cmd level (no .env in current or parent)
	utils.Log.Info("read, should be empty", utils.F{"key": "CMD_LEVEL", "value": utils.KEV.Get("CMD_LEVEL")})

	// Should get from project root
	utils.Log.Info("read", utils.F{"key": "TEST_KEY", "value": utils.KEV.Get("TEST_KEY")})

	utils.Log.Success("project root found from deep nesting")
}
