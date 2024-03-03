package main

import "os"

type Application struct {
	// The width of the terminal
	width int
	// The height of the terminal
	height int

	// If true, continue reading from reader forwards
	followMode bool

	// The input file name as given to the os.Open function for inputReader
	inputFname string

	buffer *Buffer
}

func NewApplication(width, height int, followMode bool, inputReader *os.File) (*Application, error) {
	application := &Application{
		width:      width,
		height:     height,
		followMode: followMode,
		inputFname: inputReader.Name(),
	}

	buffer, err := NewBuffer(width, height, followMode, inputReader)
	if err != nil {
		return nil, err
	}

	application.buffer = buffer

	return application, nil
}
