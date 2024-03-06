package main

import "bytes"

// Modified from bufio.ScanLines to make it always try reading more data, even
// if it's at EOF since we're expecting to work with ever growing log files.
func scanLinesEagerly(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		// We have a full newline-terminated line.
		return i + 1, data[0:i], nil
	}
	// Request more data.
	return 0, nil, nil
}
