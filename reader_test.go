package main

import (
	"io"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestReadBackwards_ReadsFromEnd(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	b := make([]byte, 2)
	n, newPos, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, n)
	assert.EqualValues(t, 3, newPos)
	assert.EqualValues(t, "lo", b)
}

func TestReadBackwards_ReadsFromMiddle(t *testing.T) {
	f, _ := createTestFile(t, "hello", 3)

	b := make([]byte, 2)
	n, newPos, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, n)
	assert.EqualValues(t, 1, newPos)
	assert.EqualValues(t, "el", b)
}

func TestReadBackwards_ReadsToStart(t *testing.T) {
	f, _ := createTestFile(t, "hello", 2)

	b := make([]byte, 2)
	n, newPos, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, n)
	assert.EqualValues(t, 0, newPos)
	assert.EqualValues(t, "he", b)
}

func TestReadBackwards_CappedByStart(t *testing.T) {
	f, _ := createTestFile(t, "hello", 2)

	b := make([]byte, 3)
	n, newPos, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, n)
	assert.EqualValues(t, 0, newPos)
	// Buffer filled from the start, so the end has padding of 0 bytes.
	assert.EqualValues(t, []byte{'h', 'e', 0}, b)
}

func TestReadBackwards_DoesNotOverwriteUnusedBuffer(t *testing.T) {
	f, _ := createTestFile(t, "hello", 2)

	b := []byte{'a', 'b', 'c'}
	n, newPos, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, n)
	assert.EqualValues(t, 0, newPos)
	// This makes sure the 'c' is not overwritten with a zero value.
	assert.EqualValues(t, "hec", b)
}

func TestReadBackwards_TrivialZeroRead(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	b := make([]byte, 0)
	n, newPos, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 0, n)
	assert.EqualValues(t, 5, newPos)
}

func TestReadBackwards_CappedZeroRead(t *testing.T) {
	f, _ := createTestFile(t, "hello")

	b := make([]byte, 2)
	n, newPos, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 0, n)
	assert.EqualValues(t, 0, newPos)
	assert.EqualValues(t, []byte{0, 0}, b)
}

func TestReadBackwards_EntirelyOutOfBounds(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	// This should overwrite the file without f knowing about it.
	assert.NoError(t, os.WriteFile(f.Name(), []byte("he"), 0644))

	b := make([]byte, 2)
	// Truncate the file to make the current seek out of bounds.
	n, newPos, err := ReadBackwards(f, b)
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, 0, n)
	assert.EqualValues(t, 5, newPos)
	assert.EqualValues(t, []byte{0, 0}, b)
}

func TestReadBackwards_ExactlyOutOfBounds(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	// This should overwrite the file without f knowing about it.
	assert.NoError(t, os.WriteFile(f.Name(), []byte("he"), 0644))

	b := make([]byte, 3)
	// Truncate the file to make the current seek out of bounds.
	n, newPos, err := ReadBackwards(f, b)
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, 0, n)
	assert.EqualValues(t, 5, newPos)
	assert.EqualValues(t, []byte{0, 0, 0}, b)
}

func TestReadBackwards_PartiallyOutOfBounds(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	// This should overwrite the file without f knowing about it.
	assert.NoError(t, os.WriteFile(f.Name(), []byte("he"), 0644))

	b := make([]byte, 4)
	// Truncate the file to make the current seek out of bounds.
	n, newPos, err := ReadBackwards(f, b)
	// EOF is NOT detected in this case. This seems like a limitation of the
	// undelying os.File reader.
	assert.NoError(t, err)
	assert.EqualValues(t, 1, n)
	// os.File does not detect that the seek is out of bounds, so as far as it
	// is concerned, its still in bounds.
	assert.EqualValues(t, 4, newPos)
	assert.EqualValues(t, []byte{'o', 0, 0, 0}, b)
}
