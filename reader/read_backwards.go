package reader

import (
	"io"
)

// BackwardsReadResult is a result of reading backwards.
type BackwardsReadResult struct {
	// N is the amount of bytes read.
	N int
	// NextPos is the position of the first byte that was read. This is the
	// position you should pass to the next call to ReadBackwardsFrom to
	// continue reading. This may be -1 if [ReadBackwards] failed to determine its
	// current seek position so it is impossible to determine the next position.
	NextPos int64
	// Seeked is true if a seek operation was performed.
	//
	// Note: When this is true you can assume the reader's actual seek position
	// is NextPos plus N. When this is false, the reader's seek position is
	// unchanged.
	Seeked bool
	// LeftToRead keeps track of how many bytes are left to read. In case of a
	// short read where fewer bytes are returned than requested, this will be
	// the number of bytes left to read. When it is -1 it means no read attempt
	// was made.
	LeftToRead int
}

// ReadBackwards reads backwards up to len(buf) bytes from the given reader.
//
// Reading backwards is implemented by first determining the current seek
// position, then calling ReadBackwardsFrom. See [ReadBackwardsFrom] for more
// info.
//
// If failed to determine the current seek position, the result's NextPos will be -1.
func ReadBackwards(reader io.ReadSeeker, buf []byte) (BackwardsReadResult, error) {
	curPos, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return BackwardsReadResult{N: 0, NextPos: -1, Seeked: false, LeftToRead: -1}, err
	}

	result, err := ReadBackwardsFrom(reader, curPos, buf)
	if !result.Seeked {
		result.NextPos = curPos
		result.Seeked = true
	}

	return result, err
}

// ReadBackwardsFrom reads backwards up to len(buf) bytes from the given reader,
// starting from a given position.
//
// Reading backwards is implemented by seeking to fromPos minus len(buf) (or up
// to the start of the file), then reading forwards normally using
// [io.ReadSeeker.Read].
//
// To continue reading backwards, pass the result's NextPos to the next call to
// ReadBackwardsFrom as fromPos:
//
//	result, _ := ReadBackwardsFrom(reader, somePos, buf1)
//	ReadBackwardsFrom(reader, result.NextPos, buf2)
//
// Note: in case of a partial read (where fewer bytes were returned than
// requested), this function makes no attempt to read the remaining bytes
// because the file may change between reads which would make the read
// inconsistent. Instead, the result's LeftToRead will be set to the number of
// bytes left to read and it is up to the caller to decide what to do.
func ReadBackwardsFrom(reader io.ReadSeeker, fromPos int64, buf []byte) (BackwardsReadResult, error) {
	if fromPos < 0 {
		panic("fromPos must be non-negative")
	}

	requested := len(buf)

	// When we can determine that a read is trivial (starting from 0, or given a
	// buffer size of 0) we early out.
	if fromPos == 0 || requested == 0 {
		return BackwardsReadResult{N: 0, NextPos: fromPos, Seeked: false, LeftToRead: -1}, nil
	}

	var leftToRead int
	if fromPos < int64(requested) {
		leftToRead = int(fromPos)
	} else {
		leftToRead = requested
	}

	nextPos := fromPos - int64(leftToRead)
	_, err := reader.Seek(nextPos, io.SeekStart)
	if err != nil {
		return BackwardsReadResult{
			N:          0,
			NextPos:    -1,
			Seeked:     false, // Assume no seek was performed on error.
			LeftToRead: -1,
		}, err
	}

	// // Attempt to read forwards up to 5 times in case fewer bytes are read than
	// // requested.
	// n := 0
	// totalRead := 0
	// attemptsLeft := 5
	// for ; attemptsLeft > 0 && leftToRead > 0; attemptsLeft-- {
	// 	n, err = reader.Read(buf[totalRead : totalRead+leftToRead])

	// 	totalRead += n
	// 	leftToRead -= n
	// 	if leftToRead < 0 {
	// 		err = io.ErrShortBuffer
	// 	}

	// 	if err != nil {
	// 		break
	// 	}
	// }

	totalRead, err := reader.Read(buf[:leftToRead])
	leftToRead -= totalRead

	return BackwardsReadResult{
		N:          totalRead,
		NextPos:    nextPos,
		Seeked:     true,
		LeftToRead: leftToRead,
	}, err
}
