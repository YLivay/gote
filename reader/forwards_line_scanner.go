package reader

import (
	"bufio"
	"bytes"
	"io"
)

type ForwardsLineScanner struct {
	*bufio.Scanner
	r           io.Reader
	token       []byte
	isCarryOver bool
}

func NewForwardsLineScanner(reader io.Reader) *ForwardsLineScanner {
	scanner := &ForwardsLineScanner{
		r:           reader,
		token:       make([]byte, 0),
		isCarryOver: false,
	}
	scanner.initInternalScanner()
	return scanner
}

func (s *ForwardsLineScanner) initInternalScanner() {
	scanner := bufio.NewScanner(s.r)
	scanner.Split(scanLines)
	s.Scanner = scanner
}

func (s *ForwardsLineScanner) Scan() bool {
	res := s.Scanner.Scan()

	// Make sure to reset our token if we're not carrying over.
	if !s.isCarryOver {
		s.token = nil
	}

	// The scanner may reach an actual EOF if it is the very first read
	// attempt of this scanner, or if the previous read ended EXACTLY on EOF
	// (which means the current one read 0 bytes).
	if !res && s.Scanner.Err() == nil {
		s.initInternalScanner()
		return false
	}

	// TODO: figure out if we have to check s.Scanner.Err() first.
	bytes := s.Scanner.Bytes()
	if len(bytes) != 0 {
		if s.isCarryOver {
			s.token = append(s.token, bytes...)
		} else {
			s.token = bytes
		}

		// If we encountered a partial token (doesn't end with a newline) it
		// means this is the last token the current scanner can read.
		//
		// In order to read past this EOF we need to reinitialize the scanner,
		// and save the partial token for the next scan.
		if bytes[len(bytes)-1] != '\n' {
			s.isCarryOver = true
			s.initInternalScanner()

			// We need to emulate the behavior of bufio.Scanner.Scan() which
			// returns false when it reaches EOF.
			return false
		} else {
			s.isCarryOver = false
			// Get rid of the newline character at the end.
			s.token = s.token[:len(s.token)-1]
		}
	}

	return true
}

func (s *ForwardsLineScanner) Bytes() []byte {
	if s.isCarryOver {
		return nil
	}

	return s.token
}

func (s *ForwardsLineScanner) Text() string {
	if s.isCarryOver {
		return ""
	}

	return string(s.token)
}

// Modified from bufio.ScanLines to make not drop carriage returns and also
// return the newline character itself. This lets us differentiate between a
// line that is returned because it has a newline character and a line that is
// returned because it reached EOF.
func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		// We have a full newline-terminated line.
		return i + 1, data[0 : i+1], nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}
