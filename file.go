package utils

import "os"

type fileOps struct{}

// File provides file operations that exit on error
var File = fileOps{}

// Read reads a file and exits on error
func (fileOps) Read(path string) []byte {
	data, err := os.ReadFile(path)
	Check(err)
	return data
}

// Write writes data to a file and exits on error
func (fileOps) Write(path string, data []byte) {
	err := os.WriteFile(path, data, 0644)
	Check(err)
}

// Open opens a file and exits on error
func (fileOps) Open(path string) *os.File {
	file, err := os.Open(path)
	Check(err)
	return file
}

// Create creates a file and exits on error
func (fileOps) Create(path string) *os.File {
	file, err := os.Create(path)
	Check(err)
	return file
}

// Exists checks if file exists
func (fileOps) Exists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// Remove removes a file and exits on error
func (fileOps) Remove(path string) {
	err := os.Remove(path)
	Check(err)
}

// Copy copies a file and exits on error
func (f fileOps) Copy(src, dst string) {
	data := f.Read(src)
	f.Write(dst, data)
}