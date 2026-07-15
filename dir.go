package utils

import (
	"os"
	"path/filepath"
)

type dirOps struct{}

// Dir provides directory operations that panic on error.
var Dir = dirOps{}

// Creates parents too, like mkdir -p.
func (dirOps) Create(path string) {
	Check(os.MkdirAll(path, 0755))
}

// Exists is false for files - that's File.Exists.
func (dirOps) Exists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// Removes contents recursively, like rm -rf.
func (dirOps) Remove(path string) {
	Check(os.RemoveAll(path))
}

func (dirOps) List(path string) []string {
	entries, err := os.ReadDir(path)
	Check(err)

	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

func (d dirOps) ListFull(path string) []string {
	names := d.List(path)
	paths := make([]string, len(names))
	for i, name := range names {
		paths[i] = filepath.Join(path, name)
	}
	return paths
}

func (d dirOps) Copy(src, dst string) {
	d.Create(dst)

	for _, entry := range d.List(src) {
		srcPath := filepath.Join(src, entry)
		dstPath := filepath.Join(dst, entry)

		if d.Exists(srcPath) {
			d.Copy(srcPath, dstPath)
		} else {
			File.Copy(srcPath, dstPath)
		}
	}
}

func (dirOps) Current() string {
	dir, err := os.Getwd()
	Check(err)
	return dir
}

func (dirOps) Change(path string) {
	Check(os.Chdir(path))
}
