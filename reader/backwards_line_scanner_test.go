package reader

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBackwardsLineScanner_ReadsSingleLine_SingleChunk(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1024)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLineScanner_ReadsSingleLine_TwoChunks(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 3)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLineScanner_ReadsSingleLine_ThreeChunks(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 2)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLineScanner_ReadsSingleLine_ManyChunks(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLine_ReadsOneLine(t *testing.T) {
	f, _ := createTestFile(t, "hi\nhello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1024)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLine_ReadsEmptyLine(t *testing.T) {
	f, _ := createTestFile(t, "hello\n", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1024)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "", bytes)
}

func TestBackwardsLine_ReadsOneLine_WithoutLastNewLine(t *testing.T) {
	f, _ := createTestFile(t, "\nhello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1024)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLine_ReadsTwoLines_SingleChunk(t *testing.T) {
	f, _ := createTestFile(t, "hi\nhello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1024)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
	bytes, err = s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hi", bytes)
}

func TestBackwardsLine_ReadsTwoLines_SingleChunk_PerLine(t *testing.T) {
	f, _ := createTestFile(t, "hi\nhello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 5)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
	bytes, err = s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hi", bytes)
}

// TestBackwardsLine_ReadsTwoLines_NewLineOnBorder tests that the scanner can read two lines when the newline is on the border of two chunks.
func TestBackwardsLine_ReadsTwoLines_NewLineOnBorder(t *testing.T) {
	f, _ := createTestFile(t, "hi\nheyo", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 5)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "heyo", bytes)
	bytes, err = s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hi", bytes)
}

func TestBackwardsLine_ReadsTwoLines_SharedChunk(t *testing.T) {
	f, _ := createTestFile(t, "hii\nhello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 4)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
	bytes, err = s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hii", bytes)
}

func TestBackwardsLine_ReadsTwoLines_SecondIsEmpty(t *testing.T) {
	f, _ := createTestFile(t, "\nhello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1024)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
	bytes, err = s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "", bytes)
}

func TestBackwardsLine_ReadPastEOF(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1024)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "hello", bytes)
	bytes, err = s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "", bytes)
}

func TestBackwardsLine_ReadPastEOF_NewLineBoundary(t *testing.T) {
	f, _ := createTestFile(t, "\nhello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1024)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
	bytes, err = s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "", bytes)
	bytes, err = s.ReadLine()
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, "", bytes)
}
