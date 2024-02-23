package reader

import "io"

func ReadBackwards(reader io.ReadSeeker, buf []byte) (n int, newPos int64, err error) {
	pos, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0, err
	}

	n, newPos, err = ReadBackwardsFrom(reader, pos, buf)
	// If -1 is returned it means either no seek was performed or a seek errored
	// out, so we should assume our seek position has not moved.
	if newPos == -1 {
		newPos = pos
	}
	return n, newPos, err
}

func ReadBackwardsFrom(reader io.ReadSeeker, fromPos int64, buf []byte) (n int, newPos int64, err error) {
	if fromPos < 0 {
		panic("fromPos must be non-negative")
	}

	requested := int64(len(buf))

	// When we can determine that a read is trivial (starting from 0, or given a
	// buffer size of 0) we early out. In this case we don't attempt to figure
	// out the new position, so simply return -1.
	if fromPos == 0 || requested == 0 {
		return 0, -1, nil
	}

	n = 0
	toRead := min(fromPos, requested)
	_, err = reader.Seek(fromPos-toRead, io.SeekStart)
	if err != nil {
		// In case of a seek error return -1 to indicate that the position has
		// not changed.
		return n, -1, err
	}

	n, err = reader.Read(buf[:toRead])
	return n, fromPos - toRead + int64(n), err
}
