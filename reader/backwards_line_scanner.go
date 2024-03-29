package reader

import (
	"bytes"
	"errors"
	"fmt"
	"io"
)

// ErrUseAfterClose is returned when the scanner is used after Close() was called.
var ErrUseAfterClose = fmt.Errorf("scanner used after Close()")

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

func NewBackwardsLineScanner(reader io.ReadSeeker, chunkSize int, seekAndWhence ...int64) (*BackwardsLineScanner, error) {
	var seek, whence int64
	switch len(seekAndWhence) {
	case 0:
		seek = 0
		whence = io.SeekEnd
	case 1:
		seek = seekAndWhence[0]
		if seek >= 0 {
			whence = io.SeekStart
		} else {
			whence = io.SeekEnd
			seek = -seek
		}
	case 2:
		seek = seekAndWhence[0]
		whence = seekAndWhence[1]
	default:
		panic("Too many arguments")
	}

	pos, err := reader.Seek(int64(seek), int(whence))
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

func (s *BackwardsLineScanner) Close() error {
	s.chunks = nil
	s.lastErr = ErrUseAfterClose
	return nil
}

// ReadLine reads the next line from the file, starting from the current
// position. It returns the line, the position in the file where the line
// starts, and an error if any occured. If the end of the file is reached, the
// error will be io.EOF. If a non io.EOF error has occured, no line data will be
// returned and the position will be -1.
func (s *BackwardsLineScanner) ReadLine() ([]byte, int64, error) {
	var err error

	if s.lastErr == ErrUseAfterClose {
		return nil, -1, s.lastErr
	}

	// Read more data if we didn't find a newline yet.
	if s.nextNewLine == -1 {
		_, err = s.readMore()
		if err != nil {
			s.lastErr = err
			if err != io.EOF {
				return nil, -1, err
			}
		}
	}

	numChunks := len(s.chunks)

	// If the last chunk started with a
	if numChunks == 0 {
		return []byte{}, 0, io.EOF
	}

	curChunk := s.chunks[numChunks-1]
	var nlIdx int
	if s.nextNewLine == -1 {
		nlIdx = bytes.LastIndexByte(curChunk.buf, '\n')
	} else {
		nlIdx = s.nextNewLine
	}

	// If we found a newline or we reached the start of the file, start
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

		if nlIdx != -1 {
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

		lineStartedAt := s.nextPos + int64(nlIdx) + 1

		return line, lineStartedAt, err
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
		if err == nil {
			err = errors.New("no error was returned")
		}

		return n, fmt.Errorf("expected to read %d bytes, but only read %d: %w", s.chunkSize, n, err)
	}

	return n, err
}
