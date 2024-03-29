package reader

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestForwardsLineScanner_ReadsLine(t *testing.T) {
	f, _ := createTestFile(t, "hello\nyou\n")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hello", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsTwoLines(t *testing.T) {
	f, _ := createTestFile(t, "hello\nyou\n")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hello", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "you", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_FindsEOF(t *testing.T) {
	f, _ := createTestFile(t, "hello")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_FindsEOFAgain(t *testing.T) {
	f, _ := createTestFile(t, "hello")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsLineEndingAtEOF(t *testing.T) {
	f, _ := createTestFile(t, "hi\n")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hi", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsPastEOF(t *testing.T) {
	f, _ := createTestFile(t, "hi")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())

	appendToTestFile(t, f, "ya\n")

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hiya", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsWellPastEOF(t *testing.T) {
	f, _ := createTestFile(t, "hi")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())

	appendToTestFile(t, f, "ya\nwhats up\nmore data")

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hiya", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "whats up", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsPastStickyEOF(t *testing.T) {
	f, _ := createTestFile(t, "hi")

	scanner := NewForwardsLineScanner(f)
	// Trying to scan again makes sure that the data from the first scanner carries over multiple empty scans.
	for i := 0; i < 3; i++ {
		res := scanner.Scan()
		assert.False(t, res)
		assert.Nil(t, scanner.Bytes())
		assert.NoError(t, scanner.Err())
	}

	appendToTestFile(t, f, "ya\n")

	res := scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hiya", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsPastMultipleEOFsDuringOneLine(t *testing.T) {
	f, _ := createTestFile(t, "hi ")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())

	// Trying to scan again makes sure that the data from the first scanner carries over multiple empty scans.
	for i := 1; i <= 3; i++ {
		appendToTestFile(t, f, fmt.Sprint(i))
		res = scanner.Scan()
		assert.False(t, res)
		assert.Nil(t, scanner.Bytes())
		assert.NoError(t, scanner.Err())
	}

	appendToTestFile(t, f, "\nsup")

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hi 123", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsPastEOF_AtBoundary(t *testing.T) {
	f, _ := createTestFile(t, "hi")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())

	appendToTestFile(t, f, "\n")

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hi", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsEmptyLines(t *testing.T) {
	f, _ := createTestFile(t, "hi\n\n\nya\n")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hi", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "ya", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsEmptyLinesPastEOF(t *testing.T) {
	f, _ := createTestFile(t, "hi")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())

	appendToTestFile(t, f, "\nya\n")

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hi", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "ya", scanner.Text())
	assert.NoError(t, scanner.Err())
}

func TestForwardsLineScanner_ReadsEmptyLinesPastEOFAtEmptyLine(t *testing.T) {
	f, _ := createTestFile(t, "hi\n")

	scanner := NewForwardsLineScanner(f)
	res := scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "hi", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.False(t, res)
	assert.Nil(t, scanner.Bytes())
	assert.NoError(t, scanner.Err())

	appendToTestFile(t, f, "\nya\n")

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "", scanner.Text())
	assert.NoError(t, scanner.Err())

	res = scanner.Scan()
	assert.True(t, res)
	assert.EqualValues(t, "ya", scanner.Text())
	assert.NoError(t, scanner.Err())
}
