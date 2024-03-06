package main

import (
	"io"
	"os"
	"path"
	"testing"
)

// CreateTestFiles creates a temporary test file with the given contents and
// seek. It returns the open file handle and the seek position from the start of
// the file. If no seek was given, it defaults to the start of the file.
func createTestFile(t *testing.T, contents string, seekStuff ...int) (*os.File, int64) {
	filepath := path.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(filepath, []byte(contents), 0644); err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	f, err := os.Open(filepath)
	if err != nil {
		t.Fatalf("Failed to open temp file: %v", err)
	}

	var seek, whence int
	switch len(seekStuff) {
	case 0:
		seek = 0
		whence = io.SeekStart
	case 1:
		seek = seekStuff[0]
		if seek >= 0 {
			whence = io.SeekStart
		} else {
			whence = io.SeekEnd
			seek = -seek
		}
	case 2:
		seek = seekStuff[0]
		whence = seekStuff[1]
	default:
		panic("Too many arguments")
	}

	var pos int64
	if seek != 0 || whence != io.SeekStart {
		pos, err = f.Seek(int64(seek), whence)
		if err != nil {
			t.Fatalf("Failed to seek temp file: %v", err)
		}
	} else {
		pos = 0
	}

	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Fatalf("Failed to close temp file: %v", err)
		}
	})

	return f, pos
}
