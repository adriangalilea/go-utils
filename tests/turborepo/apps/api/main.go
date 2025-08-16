package main

import (
	utils "github.com/adriangalilea/go-utils"
)

func main() {
	utils.Log.Info("=== Testing Turborepo Monorepo Support ===")
	utils.Log.Info("Sources:", utils.KEV.Source.List())
	
	// Should find monorepo root variable
	monorepo := utils.KEV.Get("MONOREPO_ROOT")
	utils.Log.Info("MONOREPO_ROOT =", monorepo, "(should be 'true')")
	
	// Should find API-specific variable
	apiSpecific := utils.KEV.Get("API_SPECIFIC")
	utils.Log.Info("API_SPECIFIC =", apiSpecific, "(should be 'true')")
	
	// Test precedence - local should win
	shared := utils.KEV.Get("SHARED_KEY")
	utils.Log.Info("SHARED_KEY =", shared, "(should be 'from_api_app')")
	
	// Should find monorepo-only variable
	dbUrl := utils.KEV.Get("DATABASE_URL")
	utils.Log.Info("DATABASE_URL =", dbUrl, "(should be 'postgres://monorepo')")
	
	// Source tracking
	utils.Log.Info("\n=== Source Tracking ===")
	utils.Log.Info("MONOREPO_ROOT from:", utils.KEV.SourceOf("MONOREPO_ROOT"))
	utils.Log.Info("API_SPECIFIC from:", utils.KEV.SourceOf("API_SPECIFIC"))
	utils.Log.Info("SHARED_KEY from:", utils.KEV.SourceOf("SHARED_KEY"))
	utils.Log.Info("DATABASE_URL from:", utils.KEV.SourceOf("DATABASE_URL"))
}