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
	application *Application

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

	records *bufferRecordList
}

func NewBuffer(application *Application) (*Buffer, error) {
	fwdReader, err := os.Open(application.inputFname)
	if err != nil {
		return nil, err
	}

	bkdReader, err := os.Open(application.inputFname)
	if err != nil {
		return nil, err
	}

	return &Buffer{
		application: application,
		mu:          &sync.Mutex{},
		fwdReader:   fwdReader,
		bkdReader:   bkdReader,
		records:     NewBufferRecordList(),
	}, nil
}

func (b *Buffer) setLocks() func() {
	b.mu.Lock()

	return func() {
		b.mu.Unlock()
	}
}

// SeekAndPopulate seeks the readers to the given position and populates the buffer.
//
// It works like this:
//   - Seeks to pos.
//   - Reads backwards until a new line is found or the start of the file is reached.
//   - Calculate how many lines the buffer should try reading in both directions,
//     starting from the position the backwards scanner got to.
func (b *Buffer) SeekAndPopulate(pos int64) error {
	if err := b.seekAndOrient(pos); err != nil {
		return fmt.Errorf("failed to orient buffer: %w", err)
	}

	unlock := b.setLocks()
	result := b.records.WithLock(func(records *bufferRecordList) any {
		records.Clear()
		linesBack, linesForwards := b.calcLinesToReadUsingRecords(records)
		return []int{linesBack, linesForwards}
	})
	unlock()

	// Sanity check.
	cast, ok := result.([]int)
	if !ok || len(cast) != 2 {
		panic("unexpected type")
	}

	linesBack, linesForwards := cast[0], cast[1]

	// TODO: move these to an async goroutine.
	for i := 0; i < linesBack; i++ {
		line, pos, err := b.bkdScanner.ReadLine()
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to populate buffer (backwards read): %w", err)
		}

		b.records.Prepend(newRecord(pos, line, b.application.width))
		if errors.Is(err, io.EOF) {
			break
		}
	}

	for i := 0; i < linesForwards; i++ {
		if !b.fwdScanner.Scan() {
			if err := b.fwdScanner.Err(); err != nil {
				return fmt.Errorf("failed to populate buffer (forwards read): %w", err)
			}

			break
		}

		line := b.fwdScanner.Bytes()
		b.records.Append(newRecord(-1, line, b.application.width))
	}

	// b.populateBufferTop()
	// b.populateBufferBottom()

	return nil
}

// seekAndOrient seeks to a given position and scans backwards until it reaches an end
// of line or the start of the file. That new position is where the forwards and
// backwards readers will start reading from.
//
// This function also reinstantiates the forwards and backwards scanners.
func (b *Buffer) seekAndOrient(pos int64) error {
	unlock := b.setLocks()
	defer unlock()

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

// calcLinesToRead calculates how many lines the buffer should read above or
// below its current positions. Note: lines, not records.
func (b *Buffer) calcLinesToReadUsingRecords(records *bufferRecordList) (bkdLines, fwdLines int) {
	// Figure out how many lines we have above, below and on the screen.
	aboveScreen, onScreen, belowScreen := b.calcScreenLines(records)

	return b.calcLinesToReadUsingAvailableLines(aboveScreen, onScreen, belowScreen)
}

func (b *Buffer) calcLinesToReadUsingAvailableLines(aboveScreen, onScreen, belowScreen int) (bkdLines, fwdLines int) {
	bkdLines = max(b.bkdEager-aboveScreen, 0)
	if b.application.followMode {
		// In follow mode we are interested in reading all available input as fast
		// as possible below the screen.
		fwdLines = 10000
	} else {
		// In non-follow mode we are interested in reading ahead of both the top and
		// bottom of the screen.
		fwdLines = b.application.height - onScreen + max(b.fwdEager-belowScreen, 0)
	}
	return
}

// calcScreenLines figures out how many lines we have above, on, and below the
// screen.
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
	onScreen = min(belowScreen, b.application.height)
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
		if !b.application.followMode {
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
