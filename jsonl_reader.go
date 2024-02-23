package main

import (
	"bufio"
	"errors"
	"io"
	"os"
)

type JsonlReader struct {
	file *os.File
	seek int64
	bin  *bufio.Scanner
}

func NewJsonlReader(file *os.File) (*JsonlReader, error) {
	curSeek, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, errors.New("Failed to seek input: " + err.Error())
	}

	bin := bufio.NewScanner(file)
	bin.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	bin.Split(bufio.ScanLines)

	reader := &JsonlReader{file: file, seek: curSeek, bin: bin}
	return reader, nil
}

func (r *JsonlReader) PrevJson() (any, error) {
	return nil, nil
}

func (r *JsonlReader) NextJson() (any, error) {
	return nil, nil
}

type OnScreenBuffer struct {
	lines []any

	// The byte offset in the input file where the first line starts. Reading
	// previous should start from start-1.
	start int64
	// The byte offset in the input file where the last line ends. Reading next
	// should start from end+1.
	end int64
}
