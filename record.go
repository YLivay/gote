package main

type record struct {
	// Byte offset of the start of the record in the input file.
	byteOffset int64

	// The buffer that holds the record as read from the input file.
	buf []byte

	// The lines that make up the record after they've been wrapped to fit the
	// terminal's width.
	lines []string

	// A struct that holds the parsed record.
	parsed any
}

func newRecord(byteOffset int64, buf []byte, wrapWidth int) *record {
	return &record{
		byteOffset: byteOffset,
		buf:        buf,
		lines:      WordWrap(string(buf), wrapWidth),
	}
}
