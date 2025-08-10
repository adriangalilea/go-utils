package utils

import (
	"os"
	"path/filepath"
)

type dirOps struct{}

// Dir provides directory operations that exit on error
var Dir = dirOps{}

// Create creates a directory (including parents) and exits on error
func (dirOps) Create(path string) {
	err := os.MkdirAll(path, 0755)
	Check(err)
}

// Exists checks if directory exists
func (dirOps) Exists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// Remove removes a directory and all its contents, exits on error
func (dirOps) Remove(path string) {
	err := os.RemoveAll(path)
	Check(err)
}

// List returns all entries in a directory, exits on error
func (dirOps) List(path string) []string {
	entries, err := os.ReadDir(path)
	Check(err)
	
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

// ListFull returns full paths of all entries in a directory
func (d dirOps) ListFull(path string) []string {
	names := d.List(path)
	paths := make([]string, len(names))
	for i, name := range names {
		paths[i] = filepath.Join(path, name)
	}
	return paths
}

// Copy copies a directory recursively, exits on error
func (d dirOps) Copy(src, dst string) {
	d.Create(dst)
	
	entries := d.List(src)
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry)
		dstPath := filepath.Join(dst, entry)
		
		if d.Exists(srcPath) {
			d.Copy(srcPath, dstPath)
		} else {
			File.Copy(srcPath, dstPath)
		}
	}
}

// Current returns the current working directory, exits on error
func (dirOps) Current() string {
	dir, err := os.Getwd()
	Check(err)
	return dir
}

// Change changes the current working directory, exits on error
func (dirOps) Change(path string) {
	err := os.Chdir(path)
	Check(err)
}