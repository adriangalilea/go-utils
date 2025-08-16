package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// findProjectRoot walks up from current directory looking for project markers.
// Returns the project root path or empty string if not in a project.
// Checks for: go.mod, .git, package.json
func findProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	
	return findProjectRootFrom(dir)
}

// findProjectRootFrom walks up from the given directory looking for project markers
func findProjectRootFrom(startDir string) string {
	dir := startDir
	
	for {
		// Check for project markers
		if File.Exists(filepath.Join(dir, "go.mod")) {
			return dir
		}
		if Dir.Exists(filepath.Join(dir, ".git")) {
			return dir
		}
		if File.Exists(filepath.Join(dir, "package.json")) {
			return dir
		}
		
		// Move up one directory
		parent := filepath.Dir(dir)
		
		// Stop at filesystem root
		if parent == dir {
			break
		}
		
		dir = parent
	}
	
	return ""
}

// findMonorepoRoot walks up from current directory looking for turborepo root.
// Returns the monorepo root path or empty string if not in a monorepo.
// Checks for: turbo.json (turborepo marker)
func findMonorepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	
	return findMonorepoRootFrom(dir)
}

// findMonorepoRootFrom walks up from the given directory looking for monorepo markers
func findMonorepoRootFrom(startDir string) string {
	dir := startDir
	
	for {
		// Check for turbo.json (turborepo monorepo root)
		if File.Exists(filepath.Join(dir, "turbo.json")) {
			// Verify it's actually a turborepo config
			if isTurboRepo(filepath.Join(dir, "turbo.json")) {
				return dir
			}
		}
		
		// Move up one directory
		parent := filepath.Dir(dir)
		
		// Stop at filesystem root
		if parent == dir {
			break
		}
		
		dir = parent
	}
	
	return ""
}

// isTurboRepo checks if a turbo.json file is a valid turborepo config
func isTurboRepo(turboJsonPath string) bool {
	data := File.Read(turboJsonPath)
	
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return false
	}
	
	// Check for turborepo-specific fields
	// A valid turbo.json should have "pipeline" or "tasks" field
	_, hasPipeline := config["pipeline"]
	_, hasTasks := config["tasks"]
	
	return hasPipeline || hasTasks
}