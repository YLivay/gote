package main

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestThis(t *testing.T) {
	file, _ := createTestFile(t, "0123456789abcdef\nghijklmnopqrstuv\nwxyz\n")

	buffer, err := NewBuffer(10, 10, false, file, context.Background())
	assert.NoError(t, err)

	buffer.SetEagerness(10, 10)
	err = buffer.SeekAndPopulate(17, io.SeekStart)
	assert.NoError(t, err)

	<-time.After(20 * time.Millisecond)

	lines := buffer.records.GetLinesToRender(10)
	assert.EqualValues(t, []string{"hello", "hi"}, lines)
}
