package main

import (
	"bytes"
	"fmt"
	"io"
	"slices"
)

func ReadBackwards(reader io.ReadSeeker, buf []byte) (n int, newPos int64, err error) {
	pos, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, err
	}

	n, err = ReadBackwardsFrom(reader, pos, buf)
	return n, pos - int64(n), err
}

func ReadBackwardsFrom(reader io.ReadSeeker, fromPos int64, buf []byte) (n int, err error) {
	n = 0
	requested := int64(len(buf))
	toRead := min(fromPos, requested)
	_, err = reader.Seek(fromPos-toRead, io.SeekStart)
	if err != nil {
		return n, err
	}

	if toRead == 0 {
		return 0, nil
	}

	n, err = reader.Read(buf[:toRead])
	return n, err
}

type BackwardsLineScanner struct {
	reader          io.ReadSeeker
	nextPos         int64
	readChunkSize   int
	bufSize         int
	bufUsed         int
	buf             []byte
	firstChunkLen   int
	lastErr         error
	stillHasNewLine bool
}

func NewBackwardsLineScanner(reader io.ReadSeeker, readChunkSize int) (*BackwardsLineScanner, error) {
	pos, err := reader.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}

	scanner := &BackwardsLineScanner{
		reader:          reader,
		nextPos:         pos,
		readChunkSize:   readChunkSize,
		bufSize:         readChunkSize * 2,
		bufUsed:         0,
		buf:             make([]byte, readChunkSize*2),
		firstChunkLen:   readChunkSize,
		lastErr:         nil,
		stillHasNewLine: false,
	}

	return scanner, nil
}

func (s *BackwardsLineScanner) ReadLine() (line []byte, err error) {
	var n int
	if s.stillHasNewLine {
		n = s.readChunkSize
	} else {
		n, err = s.readMore()
		if err != nil {
			s.lastErr = err
			if err != io.EOF {
				return nil, err
			}
		}
	}

	chunkRead := s.buf[s.bufUsed-s.readChunkSize : s.bufUsed-s.readChunkSize+n]
	nlIdx := bytes.LastIndexByte(chunkRead, '\n')
	// If we have a newline or we reached the start of the file
	if nlIdx != -1 || err == io.EOF {
		// Start constructing the result line.
		lastBytes := chunkRead[nlIdx+1:]
		line = make([]byte, 0, s.bufUsed)

		// Add the last bytes after the new line in the last chunk read. This is
		// the start of the result line.
		line = append(line, lastBytes...)

		// Copy the middle chunks into the result line in reverse order.
		for i := s.bufUsed - s.readChunkSize; i > s.readChunkSize; i -= s.readChunkSize {
			line = append(line, s.buf[i-s.readChunkSize:i]...)
		}

		// Add the first chunk into the result line. The first chunk may have
		// had some bytes that belonged to the previously read line, so make
		// sure to skip these.
		line = append(line, s.buf[:s.firstChunkLen]...)

		// Cleanup to prep for the next read.
		//
		// If we didn't extend the previous buffer, we can reuse the existing
		// one, otherwise we make a new one so GC can clean up the old one to
		// reclaim memory.
		var newBuf []byte
		if s.bufSize != s.readChunkSize*2 {
			s.bufSize = s.readChunkSize * 2
			s.buf = make([]byte, s.bufSize)
		}

		if nlIdx > 0 {
			// We need to save the bytes before the new line in chunkRead. These are
			// the end of the NEXT line we'll be reading.
			endOfNextLine := chunkRead[:nlIdx]
			copy(newBuf, endOfNextLine)
			s.bufUsed = s.readChunkSize
			s.stillHasNewLine = bytes.LastIndexByte(endOfNextLine, '\n') == -1

			// Save the position of the new line in the first chunk so we can skip
			// returning bytes beyond this in the next reads.
			s.firstChunkLen = nlIdx
		} else {
			s.bufUsed = 0
			s.stillHasNewLine = false
			s.firstChunkLen = s.readChunkSize
		}

		return line, nil
	}

	return s.ReadLine()
}

func (s *BackwardsLineScanner) readMore() (int, error) {
	if s.lastErr != nil {
		return 0, s.lastErr
	}

	// If our buffer is full, double its size.
	if s.bufUsed == s.bufSize {
		s.buf = slices.Grow(s.buf, s.bufSize*2)
		s.bufSize *= 2
	}

	n, err := ReadBackwardsFrom(s.reader, s.nextPos, s.buf[s.bufUsed:s.bufUsed+s.readChunkSize])
	s.nextPos -= int64(n)

	// Even if not all bytes were read, we still need to leave room for the
	// unread bytes, so always increment by the full readChunkSize.
	s.bufUsed += s.readChunkSize

	// EOFs are not supported because it means the file got shorter after the
	// reader was initialized. This read is basically undefined behavior.
	// todo: make sure eof doesnt get returned if we read exactly until the end of the file.
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}

	// If we reached the start of the file.
	if s.nextPos == 0 {
		return n, io.EOF
	} else if n < s.readChunkSize {
		// If we didn't, but we still read less than a full chunk it means we
		var errStr string
		if err != nil {
			errStr = err.Error()
		} else {
			errStr = "no error was returned"
		}

		return n, fmt.Errorf("expected to read %d bytes, but only read %d: %s", s.readChunkSize, n, errStr)
	}

	return n, err
}
