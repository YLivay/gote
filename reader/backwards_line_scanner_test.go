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
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLineScanner_ReadsSingleLine_TwoChunks(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 3)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLineScanner_ReadsSingleLine_ThreeChunks(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 2)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", bytes)
}

func TestBackwardsLineScanner_ReadsSingleLine_ManyChunks(t *testing.T) {
	f, _ := createTestFile(t, "hello", 0, io.SeekEnd)

	s, err := NewBackwardsLineScanner(f, 1)
	assert.NoError(t, err)

	bytes, err := s.ReadLine()
	assert.NoError(t, err)
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
