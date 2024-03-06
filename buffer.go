package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/YLivay/gote/log"
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
	// The context for this buffer. when it finishes (or canceled) a best effort
	// is done to close and free resources.
	ctx context.Context

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

	// A cancel function to stop the current record population process. This
	// will be called whenever the population should reevaluate what it needs to
	// populate, etc after resizing the buffer, or changing the buffer's eager
	// settings.
	cancelPopulate context.CancelCauseFunc
}

func NewBuffer(width, height int, followMode bool, inputReader *os.File, ctx context.Context) (*Buffer, error) {
	inputFname := inputReader.Name()

	fwdReader := inputReader

	bkdReader, err := os.Open(inputFname)
	if err != nil {
		return nil, err
	}

	buffer := &Buffer{
		mu:             &sync.Mutex{},
		ctx:            ctx,
		width:          width,
		height:         height,
		followMode:     followMode,
		fwdReader:      fwdReader,
		bkdReader:      bkdReader,
		records:        NewBufferRecordList(),
		cancelPopulate: func(err error) {},
	}

	go buffer.setupAsyncReads(nil, true)

	return buffer, nil
}

func (b *Buffer) ResizeScreen(width, height int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.width = width
	b.height = height

	// TODO: rewrap records lines and possibly update the records screen top.

	b.setupAsyncReads(errors.New("screen size changed"), false)
}

func (b *Buffer) SetFollowMode(followMode bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.followMode = followMode
	b.setupAsyncReads(errors.New("follow mode changed"), false)
}

func (b *Buffer) SetEagerness(fwdEager, bkdEager int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.fwdEager = fwdEager
	b.bkdEager = bkdEager
	b.setupAsyncReads(errors.New("eagerness settings changed"), false)
}

// SeekAndPopulate seeks to the given position and populates the buffer with records.
func (b *Buffer) SeekAndPopulate(pos int64, whence int) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.cancelPopulate(errors.New("changing seek position"))

	if err := b.seekAndOrient(pos, whence); err != nil {
		return fmt.Errorf("failed to orient buffer: %w", err)
	}

	b.records.WithLock(func(records *bufferRecordList) any {
		records.Clear()
		b.bkdToRead, b.fwdToRead = b.calcLinesToReadUsingRecords(records)
		return true
	})

	b.setupAsyncReads(errors.New("changing seek position"), false)

	return nil
}

func (b *Buffer) setupAsyncReads(restartReason error, withLocks bool) context.Context {
	// In a loop, wait for something to wake you up to try and read more stuff.
	populateCtx, cancelPopulate := context.WithCancelCause(b.ctx)
	context.AfterFunc(populateCtx, func() {
		reason := populateCtx.Err()
		log.Println(reason)
	})

	// get local references to stuff within a lock to make them consistent
	// in relation to each other.
	if withLocks {
		b.mu.Lock()
	}
	b.cancelPopulate(restartReason)
	bkdToRead, fwdToRead := b.bkdToRead, b.fwdToRead
	bkdScanner, fwdScanner := b.bkdScanner, b.fwdScanner
	width, height := b.width, b.height
	followMode := b.followMode
	b.cancelPopulate = cancelPopulate
	if withLocks {
		b.mu.Unlock()
	}

	go func() {
		for i := 0; i < bkdToRead; i++ {
			if populateCtx.Err() != nil {
				return
			}

			line, pos, err := bkdScanner.ReadLine()
			if err != nil && !errors.Is(err, io.EOF) {
				b.setupAsyncReads(fmt.Errorf("failed to populate buffer (backwards read): %w", err), true)
				return
			}

			b.mu.Lock()
			if populateCtx.Err() != nil {
				b.mu.Unlock()
				return
			}
			b.records.WithLock(func(records *bufferRecordList) any {
				r := newRecord(pos, line, width)
				records.Prepend(r)

				// If prepending but we don't have a full screen of lines yet,
				// we should scroll up to try and fit more lines on screen.
				_, onScreen, _ := records.CalcScreenLines(height)
				canScroll := min(height-onScreen, len(r.lines))
				if canScroll > 0 {
					records.ScrollUp(canScroll)
				}

				return true
			})
			b.mu.Unlock()

			if errors.Is(err, io.EOF) {
				break
			}
		}
	}()

	go func() {
		for i := 0; i < fwdToRead || followMode; i++ {
			if populateCtx.Err() != nil {
				return
			}

			if !fwdScanner.Scan() {
				if err := fwdScanner.Err(); err != nil {
					b.setupAsyncReads(fmt.Errorf("failed to populate buffer (forwards read): %w", err), true)
					return
				}

				if followMode {
					// If EOF, but we're in follow mode, wait a bit and try
					// reading the file again.
					<-time.After(10 * time.Millisecond)
					continue
				} else {
					// If EOF and we're not in follow mode, stop. we have
					// all the data we wanted.
					return
				}
			}

			b.mu.Lock()
			if populateCtx.Err() != nil {
				b.mu.Unlock()
				return
			}

			line := fwdScanner.Bytes()
			record := newRecord(-1, line, width)

			b.records.WithLock(func(records *bufferRecordList) any {
				records.Append(record)
				if followMode {
					records.ScrollToBottom(height)
				}
				return true
			})
			b.mu.Unlock()
		}
	}()

	return populateCtx
}

// seekAndOrient seeks to a given position and "orients" the buffer. The
// forwards and backwards scanners are reinstantiated.
//
// orientation is done by scanning backwards until an end of line is found or
// the start of the file is reached. That new position is where the forwards and
// backwards readers will start reading from.
//
// This function is not concurrency safe.
func (b *Buffer) seekAndOrient(pos int64, whence int) error {
	// Cleanup old backwards scanner if it exists.
	if b.bkdScanner != nil {
		if err := b.bkdScanner.Close(); err != nil {
			return err
		}
	}

	bkdScanner, err := reader.NewBackwardsLineScanner(b.bkdReader, 1024, pos, int64(whence))
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
	aboveScreen, onScreen, belowScreen := records.CalcScreenLines(b.height)

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

// prune prunes the buffer to the desired size.
func (b *Buffer) prune() (int, int) {
	result := b.records.WithLock(func(records *bufferRecordList) any {
		prunedBack, prunedFwd := 0, 0
		hasAbove, hasOnScreen, hasBelow := records.CalcScreenLines(b.height)
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
