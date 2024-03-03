package reader

import (
	"io"
	"os"
	"testing"

	"github.com/YLivay/gote/utils"
	"github.com/stretchr/testify/assert"
)

func TestReadBackwards_ReadsFromEnd(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 0, io.SeekEnd)

	b := make([]byte, 2)
	result, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, result.N)
	assert.EqualValues(t, 3, result.NextPos)
	assert.EqualValues(t, "lo", b)
}

func TestReadBackwards_ReadsFromMiddle(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 3)

	b := make([]byte, 2)
	result, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, result.N)
	assert.EqualValues(t, 1, result.NextPos)
	assert.EqualValues(t, "el", b)
}

func TestReadBackwards_ReadsToStart(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 2)

	b := make([]byte, 2)
	result, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, result.N)
	assert.EqualValues(t, 0, result.NextPos)
	assert.EqualValues(t, "he", b)
}

func TestReadBackwards_CappedByStart(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 2)

	b := make([]byte, 3)
	result, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, result.N)
	assert.EqualValues(t, 0, result.NextPos)
	// Buffer filled from the start, so the end has padding of 0 bytes.
	assert.EqualValues(t, []byte{'h', 'e', 0}, b)
}

func TestReadBackwards_DoesNotOverwriteUnusedBuffer(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 2)

	b := []byte{'a', 'b', 'c'}
	result, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 2, result.N)
	assert.EqualValues(t, 0, result.NextPos)
	// This makes sure the 'c' is not overwritten with a zero value.
	assert.EqualValues(t, "hec", b)
}

func TestReadBackwards_TrivialZeroRead(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 0, io.SeekEnd)

	b := make([]byte, 0)
	result, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 0, result.N)
	assert.EqualValues(t, 5, result.NextPos)
}

func TestReadBackwards_CappedZeroRead(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello")

	b := make([]byte, 2)
	result, err := ReadBackwards(f, b)
	assert.NoError(t, err)
	assert.EqualValues(t, 0, result.N)
	assert.EqualValues(t, 0, result.NextPos)
	assert.EqualValues(t, []byte{0, 0}, b)
}

func TestReadBackwards_EntirelyOutOfBounds(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 0, io.SeekEnd)

	// This should overwrite the file without f knowing about it.
	assert.NoError(t, os.WriteFile(f.Name(), []byte("ya"), 0644))

	b := make([]byte, 2) // Basically trying to read 'lo' from 'hello'.
	result, err := ReadBackwards(f, b)
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, 0, result.N)
	// Usually when reading backwards, the seek position first moves back and
	// then reads forwards. Since no bytes were read, the seek position
	// effectively only moved back, resulting in len(hello) - len(b) = 3.
	assert.EqualValues(t, 3, result.NextPos) // todo
	assert.EqualValues(t, []byte{0, 0}, b)
}

func TestReadBackwards_ExactlyOutOfBounds(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 0, io.SeekEnd)

	// This should overwrite the file without f knowing about it.
	assert.NoError(t, os.WriteFile(f.Name(), []byte("ya"), 0644))

	b := make([]byte, 3) // Basically trying to read 'llo' from 'hello'.
	result, err := ReadBackwards(f, b)
	assert.ErrorIs(t, err, io.EOF)
	assert.EqualValues(t, 0, result.N)
	assert.EqualValues(t, 2, result.NextPos)
	assert.EqualValues(t, []byte{0, 0, 0}, b)
}

func TestReadBackwards_PartiallyOutOfBounds(t *testing.T) {
	f, _ := utils.CreateTestFile(t, "hello", 0, io.SeekEnd)

	// This should overwrite the file without f knowing about it.
	assert.NoError(t, os.WriteFile(f.Name(), []byte("ya"), 0644))

	b := make([]byte, 4)
	result, err := ReadBackwards(f, b)
	// EOF is NOT detected in this case. This seems like a limitation of the
	// underlying os.File reader.
	assert.NoError(t, err)
	assert.EqualValues(t, 1, result.N)
	assert.EqualValues(t, 1, result.NextPos) // todo

	// However the data it read IS from the new file.
	assert.EqualValues(t, []byte{'a', 0, 0, 0}, b)
}
