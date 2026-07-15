package utils

import (
	"os"
	"path/filepath"
)

type fileOps struct{}

// File provides file operations that panic on error.
var File = fileOps{}

func (fileOps) Read(path string) []byte {
	data, err := os.ReadFile(path)
	Check(err)
	return data
}

func (f fileOps) ReadString(path string) string {
	return string(f.Read(path))
}

// Write is atomic: data lands in a temp file that is fsynced and renamed
// over the target, so a crash mid-write leaves the old content, never a
// truncated file. The temp file lives in the target's directory - rename
// only guarantees atomicity within one filesystem.
func (fileOps) Write(path string, data []byte) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	Check(err)
	defer os.Remove(tmp.Name()) //nolint:errcheck // no-op after successful rename

	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	Check(tmp.Close())
	Check(writeErr)
	Check(syncErr)

	Check(os.Chmod(tmp.Name(), 0644))
	Check(os.Rename(tmp.Name(), path))
}

func (f fileOps) WriteString(path string, data string) {
	f.Write(path, []byte(data))
}

func (fileOps) Open(path string) *os.File {
	file, err := os.Open(path)
	Check(err)
	return file
}

func (fileOps) Create(path string) *os.File {
	file, err := os.Create(path)
	Check(err)
	return file
}

// Exists is false for directories - that's Dir.Exists.
func (fileOps) Exists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func (fileOps) Remove(path string) {
	Check(os.Remove(path))
}

func (f fileOps) Copy(src, dst string) {
	f.Write(dst, f.Read(src))
}
