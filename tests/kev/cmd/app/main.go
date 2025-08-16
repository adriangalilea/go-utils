package main

import (
	utils "github.com/adriangalilea/go-utils"
)

func main() {
	utils.Log.Info("=== Running from cmd/app ===")
	utils.Log.Info("Sources:", utils.KEV.Source.List())
	
	// Should find project root
	projectRoot := utils.KEV.Get("PROJECT_ROOT")
	utils.Log.Info("PROJECT_ROOT =", projectRoot)
	
	// Should find cmd level
	cmdLevel := utils.KEV.Get("CMD_LEVEL") 
	utils.Log.Info("CMD_LEVEL =", cmdLevel)
	
	// Test precedence - local .env should win
	shared := utils.KEV.Get("SHARED_KEY")
	utils.Log.Info("SHARED_KEY =", shared)
	
	// Show where they came from
	utils.Log.Info("\nSource tracking:")
	utils.Log.Info("PROJECT_ROOT from:", utils.KEV.SourceOf("PROJECT_ROOT"))
	utils.Log.Info("CMD_LEVEL from:", utils.KEV.SourceOf("CMD_LEVEL"))
	utils.Log.Info("SHARED_KEY from:", utils.KEV.SourceOf("SHARED_KEY"))
}