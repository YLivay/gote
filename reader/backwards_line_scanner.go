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
	var err error

	// Read more data if we didn't find a newline yet.
	if s.nextNewLine == -1 {
		_, err = s.readMore()
		if err != nil {
			s.lastErr = err
			if err != io.EOF {
				return nil, err
			}
		}
	}

	numChunks := len(s.chunks)

	// If the last chunk started with a
	if numChunks == 0 {
		return []byte{}, io.EOF
	}

	curChunk := s.chunks[numChunks-1]
	var nlIdx int
	if s.nextNewLine == -1 {
		nlIdx = bytes.LastIndexByte(curChunk.buf, '\n')
	} else {
		nlIdx = s.nextNewLine
	}

	// If we found a newline or we reached the start of the file, startt
	// constructing the result line from our buffers.
	if nlIdx != -1 || err == io.EOF {
		// Calculate the length of the line so we can allocate a buffer for it
		// once without reallocations.

		// The first chunk is partial. We only consider the bytes after the newline.
		lineLen := curChunk.len - nlIdx - 1
		// The rest of the chunks are full, so we consider all of their bytes.
		for i := numChunks - 2; i >= 0; i-- {
			lineLen += s.chunks[i].len
		}
		line := make([]byte, lineLen)

		// Copy the bytes from the chunks into the result line.
		written := copy(line, curChunk.buf[nlIdx+1:curChunk.len]) // Note, the first chunk is partial.
		// The rest of the chunks are full.
		for i := numChunks - 2; i >= 0; i-- {
			written += copy(line[written:], s.chunks[i].buf[:s.chunks[i].len])
		}

		// Cleanup to prep for the next read.
		s.chunks = make([]*readChunk, 0)

		if nlIdx >= 0 {
			// We need to save the bytes before the new line in curChunk.buf. These are
			// the end of the NEXT line we'll be reading.
			var remainingChunk *readChunk
			if nlIdx > 0 {
				remainingChunk = &readChunk{
					buf: curChunk.buf[:nlIdx],
					len: nlIdx,
				}
			} else {
				remainingChunk = &readChunk{
					buf: []byte{},
					len: 0,
				}
			}

			s.chunks = append(s.chunks, remainingChunk)
			s.nextNewLine = bytes.LastIndexByte(remainingChunk.buf, '\n')
		} else {
			s.nextNewLine = -1
		}

		// Do not return EOF if we still have data to read.
		if nlIdx != -1 && err == io.EOF {
			err = nil
		}

		return line, err
	}

	return s.ReadLine()
}

func (s *BackwardsLineScanner) readMore() (int, error) {
	if s.lastErr != nil {
		return 0, s.lastErr
	}

	buf := make([]byte, s.chunkSize)
	result, err := ReadBackwardsFrom(s.reader, s.nextPos, buf)
	n := result.N

	// In case of a partial read, try reading the remaining bytes.
	leftToRead := result.LeftToRead
	if leftToRead > 0 {
		// If no data is returned at all for a few consecutive reads, we stop
		// trying and return io.ErrNoProgress.
		emptyReads := 0
		maxEmptyReads := 10
		nPart := 0
		for leftToRead > 0 {
			nPart, err = s.reader.Read(buf[n : n+leftToRead])

			n += nPart
			leftToRead -= nPart

			if err != nil {
				break
			}

			// Sanity check for bad readers.
			if leftToRead < 0 {
				err = io.ErrShortBuffer
				break
			}

			if nPart == 0 {
				emptyReads++
				if emptyReads >= maxEmptyReads {
					err = io.ErrNoProgress
					break
				}
			} else {
				emptyReads = 0
			}
		}
	}

	s.nextPos = result.NextPos

	s.chunks = append(s.chunks, &readChunk{
		buf: buf,
		len: n,
	})

	// EOFs are not supported because it means the file got shorter after the
	// reader was initialized. This read is basically undefined behavior.
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}

	// If we reached the start of the file.
	if s.nextPos == 0 {
		return n, io.EOF
	} else if leftToRead > 0 {
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
