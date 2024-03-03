package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/YLivay/gote/reader"
)

type Buffer struct {
	// The terminal width. Records will be wrapped to lines of this length.
	width int
	// The terminal height. This is used to calculate how many lines are
	// actually visible on screen.
	height int
	// If true, will continue reading from the input file forwards and scroll to
	// keep the last line of the last record on the screen.
	followMode bool

	// Mutex to serialize operations.
	mu *sync.Mutex

	// A reader for reading forwards in the file. This reader is rarely expected
	// to perform seek operations.
	fwdReader *os.File
	// A scanner that reads forwards from fwdReader line by line.
	fwdScanner *bufio.Scanner
	// A reader for reading backwards in the file. This reader needs to do
	// nearly as much seeks as it does reads.
	bkdReader *os.File
	// A scanner that reads backwards from bkdReader line by line.
	bkdScanner *reader.BackwardsLineScanner

	// How many lines to eagerly preload ahead of the bottom of the screen.
	fwdEager int
	// How many lines to eagerly preload ahead of the top of the screen.
	bkdEager int

	// How many lines to read forwards in order to fill the eager buffer.
	fwdToRead int
	// How many lines to read backwards in order to fill the eager buffer.
	bkdToRead int

	// The managed list of records loaded by this buffer's scanners.
	records *bufferRecordList
}

func NewBuffer(width, height int, followMode bool, inputFname string) (*Buffer, error) {
	fwdReader, err := os.Open(inputFname)
	if err != nil {
		return nil, err
	}

	bkdReader, err := os.Open(inputFname)
	if err != nil {
		return nil, err
	}

	return &Buffer{
		mu:         &sync.Mutex{},
		width:      width,
		height:     height,
		followMode: followMode,
		fwdReader:  fwdReader,
		bkdReader:  bkdReader,
		records:    NewBufferRecordList(),
	}, nil
}

// SeekAndPopulate seeks to the given position and populates the buffer with records.
func (b *Buffer) SeekAndPopulate(pos int64) error {
	if err := b.seekAndOrient(pos); err != nil {
		return fmt.Errorf("failed to orient buffer: %w", err)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.records.WithLock(func(records *bufferRecordList) any {
		records.Clear()
		b.bkdToRead, b.fwdToRead = b.calcLinesToReadUsingRecords(records)
		return true
	})

	b.something()

	return nil
}

func (b *Buffer) something() error {
	// TODO: move these to an async goroutine.
	for i := 0; i < b.bkdToRead; i++ {
		line, pos, err := b.bkdScanner.ReadLine()
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to populate buffer (backwards read): %w", err)
		}

		b.records.Prepend(newRecord(pos, line, b.width))
		if errors.Is(err, io.EOF) {
			break
		}
	}

	for i := 0; i < b.fwdToRead; i++ {
		if !b.fwdScanner.Scan() {
			if err := b.fwdScanner.Err(); err != nil {
				return fmt.Errorf("failed to populate buffer (forwards read): %w", err)
			}

			break
		}

		line := b.fwdScanner.Bytes()
		b.records.Append(newRecord(-1, line, b.width))
	}

	// b.populateBufferTop()
	// b.populateBufferBottom()

	return nil
}

// seekAndOrient seeks to a given position and "orients" the buffer. The
// forwards and backwards scanners are reinstantiated.
//
// orientation is done by scanning backwards until an end of line is found or
// the start of the file is reached. That new position is where the forwards and
// backwards readers will start reading from.
func (b *Buffer) seekAndOrient(pos int64) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Cleanup old backwards scanner if it exists.
	if b.bkdScanner != nil {
		if err := b.bkdScanner.Close(); err != nil {
			return err
		}
	}

	bkdScanner, err := reader.NewBackwardsLineScanner(b.bkdReader, 1024, pos)
	if err != nil {
		return err
	}

	_, pos, err = bkdScanner.ReadLine()
	if err != nil && !errors.Is(err, io.EOF) {
		if err2 := bkdScanner.Close(); err2 != nil {
			return errors.Join(err, err2)
		}
		return err
	}

	// Start reading forwards from the position of the record.
	_, err = b.fwdReader.Seek(pos, io.SeekStart)
	if err != nil {
		return err
	}

	fwdScanner := bufio.NewScanner(b.fwdReader)
	fwdScanner.Split(reader.ScanLines)
	fwdScanner.Buffer(make([]byte, 1024), 1024*1024)

	b.bkdScanner = bkdScanner
	b.fwdScanner = fwdScanner

	return nil
}

// calcLinesToReadUsingRecords calculates how many lines the buffer should read
// above or below its current positions. This considers the already loaded lines
// and the buffer's eagerness. Note: this returns number of lines, not records.
func (b *Buffer) calcLinesToReadUsingRecords(records *bufferRecordList) (bkdLines, fwdLines int) {
	// Figure out how many lines we have above, below and on the screen.
	aboveScreen, onScreen, belowScreen := b.calcScreenLines(records)

	return b.calcLinesToReadUsingAvailableLines(aboveScreen, onScreen, belowScreen)
}

// calcLinesToReadUsingAvailableLines calculates how many lines the buffer
// should read above or below its current positions. This considers the buffer's
// eagerness. Note: this returns number of lines, not records.
func (b *Buffer) calcLinesToReadUsingAvailableLines(aboveScreen, onScreen, belowScreen int) (bkdLines, fwdLines int) {
	bkdLines = max(b.bkdEager-aboveScreen, 0)
	if b.followMode {
		// In follow mode we are interested in reading all available input as fast
		// as possible below the screen.
		fwdLines = 10000
	} else {
		// In non-follow mode we are interested in reading ahead of both the top and
		// bottom of the screen.
		fwdLines = b.height - onScreen + max(b.fwdEager-belowScreen, 0)
	}
	return
}

// calcScreenLines figures out how many lines the buffer currently has loaded
// and how many are displayed above, below and on the screen.
func (b *Buffer) calcScreenLines(records *bufferRecordList) (aboveScreen, onScreen, belowScreen int) {
	screenTop := records.screenTop
	if screenTop == nil {
		return 0, 0, 0
	}

	aboveScreen += records.screenTopOffset
	for r := screenTop.prev; r != nil; r = r.prev {
		aboveScreen += len(r.record.lines)
	}
	belowScreen += len(screenTop.record.lines) - records.screenTopOffset
	for r := screenTop.next; r != nil; r = r.next {
		belowScreen += len(r.record.lines)
	}
	onScreen = min(belowScreen, b.height)
	belowScreen -= onScreen
	return
}

// prune prunes the buffer to the desired size.
func (b *Buffer) prune() (int, int) {
	result := b.records.WithLock(func(records *bufferRecordList) any {
		prunedBack, prunedFwd := 0, 0
		hasAbove, hasOnScreen, hasBelow := b.calcScreenLines(records)
		wantsAbove, wantsBelow := b.calcLinesToReadUsingAvailableLines(hasAbove, hasOnScreen, hasBelow)

		// Prune the buffer to the desired size.
		recordLines := len(records.head.record.lines)
		for hasAbove-recordLines > wantsAbove {
			records.PopFirst()
			hasAbove -= recordLines
			recordLines = len(records.head.record.lines)
			prunedBack++
		}

		// Only prune forward buffer if we are not in follow mode.
		if !b.followMode {
			recordLines = len(records.tail.record.lines)
			for hasBelow-recordLines > wantsBelow {
				records.PopLast()
				hasBelow -= recordLines
				recordLines = len(records.tail.record.lines)
				prunedFwd++
			}
		}

		return []int{prunedBack, prunedFwd}
	})

	// Sanity check.
	cast, ok := result.([]int)
	if !ok || len(cast) != 2 {
		panic("unexpected type")
	}

	return cast[0], cast[1]
}
