package reader

import (
	"bytes"
	"fmt"
	"io"
)

type BackwardsLineScanner struct {
	reader      io.ReadSeeker
	nextPos     int64
	chunkSize   int
	chunks      []*readChunk
	nextNewLine int
	lastErr     error
}

type readChunk struct {
	buf []byte
	len int
}

func NewBackwardsLineScanner(reader io.ReadSeeker, chunkSize int) (*BackwardsLineScanner, error) {
	pos, err := reader.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	scanner := &BackwardsLineScanner{
		reader:      reader,
		nextPos:     pos,
		chunkSize:   chunkSize,
		chunks:      make([]*readChunk, 0),
		nextNewLine: -1,
		lastErr:     nil,
	}

	return scanner, nil
}

func (s *BackwardsLineScanner) ReadLine() ([]byte, error) {
	var nlIdx int
	var curChunk *readChunk
	var err error

	if s.nextNewLine != -1 {
		curChunk = s.chunks[len(s.chunks)-1]
		nlIdx = s.nextNewLine
	} else {
		_, err = s.readMore()
		if err != nil {
			s.lastErr = err
			if err != io.EOF {
				return nil, err
			}
		}

		curChunk = s.chunks[len(s.chunks)-1]
		nlIdx = bytes.LastIndexByte(curChunk.buf, '\n')
	}

	// If we have a newline or we reached the start of the file
	if nlIdx != -1 || err == io.EOF {
		// Start constructing the result line.
		lineLen := 0
		for _, chunk := range s.chunks {
			lineLen += chunk.len
		}
		line := make([]byte, lineLen)

		written := 0
		for i := len(s.chunks) - 1; i >= 0; i-- {
			written += copy(line[written:], s.chunks[i].buf[:s.chunks[i].len])
		}

		// Cleanup to prep for the next read.
		s.chunks = make([]*readChunk, 0)

		if nlIdx > 0 {
			// We need to save the bytes before the new line in curChunk.buf. These are
			// the end of the NEXT line we'll be reading.
			remainingChunk := &readChunk{
				buf: curChunk.buf[:nlIdx],
				len: nlIdx,
			}

			s.chunks = append(s.chunks, remainingChunk)
			s.nextNewLine = bytes.LastIndexByte(remainingChunk.buf, '\n')
		} else {
			s.nextNewLine = -1
		}

		return line, nil
	}

	return s.ReadLine()
}

func (s *BackwardsLineScanner) readMore() (int, error) {
	if s.lastErr != nil {
		return 0, s.lastErr
	}

	buf := make([]byte, s.chunkSize)
	n, curPos, err := ReadBackwardsFrom(s.reader, s.nextPos, buf)
	s.nextPos = curPos - int64(n)

	s.chunks = append(s.chunks, &readChunk{
		buf: buf,
		len: n,
	})

	// EOFs are not supported because it means the file got shorter after the
	// reader was initialized. This read is basically undefined behavior.
	// todo: make sure eof doesnt get returned if we read exactly until the end of the file.
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}

	// If we reached the start of the file.
	if s.nextPos == 0 {
		return n, io.EOF
	} else if n < s.chunkSize {
		// If we didn't, but we still read less than a full chunk it means we
		var errStr string
		if err != nil {
			errStr = err.Error()
		} else {
			errStr = "no error was returned"
		}

		return n, fmt.Errorf("expected to read %d bytes, but only read %d: %s", s.chunkSize, n, errStr)
	}

	return n, err
}
