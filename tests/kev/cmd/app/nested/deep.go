package main

import (
	utils "github.com/adriangalilea/go-utils"
)

func main() {
	utils.Log.Info("=== Running from cmd/app/nested (deep) ===")
	utils.Log.Info("Sources:", utils.KEV.Source.List())
	
	// Should still find project root
	projectRoot := utils.KEV.Get("PROJECT_ROOT")
	utils.Log.Info("PROJECT_ROOT =", projectRoot)
	
	// Should NOT find cmd level (no .env in current or parent)
	cmdLevel := utils.KEV.Get("CMD_LEVEL") 
	utils.Log.Info("CMD_LEVEL =", cmdLevel, "(should be empty)")
	
	// Should get from project root
	testKey := utils.KEV.Get("TEST_KEY")
	utils.Log.Info("TEST_KEY =", testKey)
	
	utils.Log.Info("\nProject root still found from deep nesting!")
}