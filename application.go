package main

type Application struct {
	// The width of the terminal
	width int
	// The height of the terminal
	height int

	// If true, continue reading from reader forwards
	followMode bool

	inputFname string

	buffer *Buffer
}

func NewApplication(width, height int, followMode bool, inputFname string) (*Application, error) {
	application := &Application{
		width:      width,
		height:     height,
		followMode: followMode,
		inputFname: inputFname,
	}

	buffer, err := NewBuffer(width, height, followMode, inputFname)
	if err != nil {
		return nil, err
	}

	application.buffer = buffer

	return application, nil
}
