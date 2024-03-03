package main

import (
	"testing"

	"github.com/YLivay/gote/utils"
	"github.com/stretchr/testify/assert"
)

func TestThis(t *testing.T) {
	file, _ := utils.CreateTestFile(t, "0123456789abcdef\nghijklmnopqrstuv\nwxyz\n")

	application, err := NewApplication(10, 10, false, file.Name())
	assert.NoError(t, err)

	buffer := application.buffer
	buffer.bkdEager = 10
	buffer.fwdEager = 10
	err = buffer.SeekAndPopulate(17)
	assert.NoError(t, err)

	lines := buffer.records.GetLinesToRender(10)
	assert.EqualValues(t, []string{"hello", "hi"}, lines)
}
