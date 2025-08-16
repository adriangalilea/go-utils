package utils

import (
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