package main

import (
	utils "github.com/adriangalilea/go-utils"
)

func main() {
	utils.Log.Info("testing turborepo monorepo support", utils.F{"sources": utils.KEV.Source.List()})

	expect := []struct{ key, want string }{
		{"MONOREPO_ROOT", "true"},               // monorepo root variable
		{"API_SPECIFIC", "true"},                // API-specific variable
		{"SHARED_KEY", "from_api_app"},          // precedence: local wins
		{"DATABASE_URL", "postgres://monorepo"}, // monorepo-only variable
	}
	for _, e := range expect {
		utils.Log.Info("read", utils.F{"key": e.key, "value": utils.KEV.Get(e.key), "want": e.want, "from": utils.KEV.SourceOf(e.key)})
	}
}
