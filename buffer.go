package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/YLivay/gote/reader"
	"github.com/gdamore/tcell/v2"
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
	fwdScanner *reader.ForwardsLineScanner
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

	// A callback to invoke when an event is received. It will be posted to the
	// application screen.
	postEvent func(tcell.Event) error

	// A mutex to serialize canceling the current populate process.
	muCancelPopulate *sync.Mutex

	// A cancel function to stop the current record population process. This
	// will be called whenever the current async readers should be disposed. For
	// example, this will be called before seeking and reorienting the buffer,
	// or on reader errors.
	cancelPopulate func(err error) <-chan any
}

func NewBuffer(width, height int, followMode bool, inputReader *os.File, ctx context.Context) (*Buffer, error) {
	inputFname := inputReader.Name()

	fwdReader := inputReader

	bkdReader, err := os.Open(inputFname)
	if err != nil {
		return nil, err
	}

	buffer := &Buffer{
		mu:         &sync.Mutex{},
		ctx:        ctx,
		width:      width,
		height:     height,
		followMode: followMode,
		fwdReader:  fwdReader,
		bkdReader:  bkdReader,
		records:    NewBufferRecordList(),
		postEvent: func(e tcell.Event) error {
			return nil
		},
		muCancelPopulate: &sync.Mutex{},
		cancelPopulate: func(err error) <-chan any {
			ch := make(chan any)
			close(ch)
			return ch
		},
	}

	buffer.setupAsyncReads(nil)

	return buffer, nil
}

// TODO: too early for me to figure out how these should work.
// func (b *Buffer) ResizeScreen(width, height int) {
// 	b.mu.Lock()
// 	defer b.mu.Unlock()

// 	b.width = width
// 	b.height = height

// 	// TODO: rewrap records lines and possibly update the records screen top.

// 	b.setupAsyncReads(errors.New("screen size changed"), false)
// }

// func (b *Buffer) SetFollowMode(followMode bool) {
// 	b.mu.Lock()
// 	defer b.mu.Unlock()

// 	b.followMode = followMode
// 	b.setupAsyncReads(errors.New("follow mode changed"), false)
// }

// func (b *Buffer) SetEagerness(fwdEager, bkdEager int) {
// 	b.mu.Lock()
// 	defer b.mu.Unlock()

// 	b.fwdEager = fwdEager
// 	b.bkdEager = bkdEager
// 	b.setupAsyncReads(errors.New("eagerness settings changed"), false)
// }

func (b *Buffer) SetPostEventFunc(postEvent func(tcell.Event) error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.postEvent = postEvent
}

// SeekAndPopulate seeks to the given position and populates the buffer with
// records. It also starts asynchronous reads to keep the buffer populated as
// you move around.
func (b *Buffer) SeekAndPopulate(pos int64, whence int) error {
	b.mu.Lock()

	<-b.cancelPopulate(errors.New("changing seek position"))

	if err := b.seekAndOrient(pos, whence); err != nil {
		b.mu.Unlock()
		return fmt.Errorf("failed to orient buffer: %w", err)
	}

	b.records.WithLock(func(records *bufferRecordList) any {
		records.Clear()
		b.bkdToRead, b.fwdToRead = b.calcLinesToReadUsingRecords(records)
		return true
	})

	b.mu.Unlock()

	b.setupAsyncReads(errors.New("changing seek position"))

	return nil
}

// setupAsyncReads sets up two separate goroutines to read from our backwards
// and forwards readers to populate the buffer with records.
//
// calls to setupAsyncReads run serially because concurrent execution can lead
// to concurrent reads on the same readers which can mangle the order of
// appending/prepending into the records buffer, but the read loop itself is
// lockless.
//
// Calling this function will cancel the current populate process before
// starting the new one.
func (b *Buffer) setupAsyncReads(restartReason error) {
	// We need setupAsyncReads to run serially to because concurrent execution
	// of setupAsyncReads can lead to concurrent reads on the same readers which
	// can mangle the order of appending/prepending into the records buffer.
	//
	// Note: this only locks the setting up of the async reads, not the read
	// loops themselves.
	b.muCancelPopulate.Lock()
	defer b.muCancelPopulate.Unlock()

	// When both readers are done we can consider this operation as done.
	bkdReaderDone := make(chan struct{})
	fwdReaderDone := make(chan struct{})
	doneCh := make(chan any)
	go func() {
		<-bkdReaderDone
		<-fwdReaderDone
		close(doneCh)
	}()

	// Used to signal the current populate process to abort.
	innerCtx, innerCancel := context.WithCancelCause(b.ctx)

	// Wrap innerCancel with a function that allows the caller to await the
	// populate process finishing.
	cancelPopulate := func(err error) <-chan any {
		innerCancel(err)
		return doneCh
	}

	oldCancelPopulate := b.cancelPopulate
	b.cancelPopulate = cancelPopulate
	<-oldCancelPopulate(restartReason)

	// By this point we are guaranteed reader exclusivity, now we need to lock
	// the buffer itself to get a consistent view of the buffer state.
	b.mu.Lock()
	defer b.mu.Unlock()

	// At this point its possible that this operation has already been canceled,
	// so check for it.
	if innerCtx.Err() != nil {
		close(bkdReaderDone)
		close(fwdReaderDone)
		return
	}

	// By this point the operation is not canceled and we have exclusive access
	// to the buffer. Set up the new readers loop.

	bkdToRead, fwdToRead := b.bkdToRead, b.fwdToRead
	bkdScanner, fwdScanner := b.bkdScanner, b.fwdScanner
	width, height := b.width, b.height
	followMode := b.followMode
	b.cancelPopulate = cancelPopulate

	go func() {
		defer close(bkdReaderDone)

		for i := 0; i < bkdToRead; i++ {
			if innerCtx.Err() != nil {
				return
			}

			line, pos, err := bkdScanner.ReadLine()
			if err != nil && !errors.Is(err, io.EOF) {
				panic(fmt.Errorf("failed to populate buffer (backwards read): %w", err))
			}

			// When EOF is returned with an empty line it doesnt necessarily
			// mean that an empty line exists at the start of the file. More
			// likely it means we didn't read anything, so avoid adding this
			// line to the buffer.
			if len(line) == 0 && errors.Is(err, io.EOF) {
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
			b.postEvent(tcell.NewEventInterrupt(nil))

			if errors.Is(err, io.EOF) {
				return
			}
		}
	}()

	go func() {
		defer close(fwdReaderDone)

		for i := 0; i < fwdToRead || followMode; i++ {
			if innerCtx.Err() != nil {
				return
			}

			if !fwdScanner.Scan() {
				if err := fwdScanner.Err(); err != nil {
					panic(fmt.Errorf("failed to populate buffer (forwards read): %w", err))
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

			line := fwdScanner.Bytes()
			record := newRecord(-1, line, width)

			b.records.WithLock(func(records *bufferRecordList) any {
				records.Append(record)
				if followMode {
					records.ScrollToBottom(height)
				}
				return true
			})
			b.postEvent(tcell.NewEventInterrupt(nil))
		}
	}()
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

	fwdScanner := reader.NewForwardsLineScanner(b.fwdReader)
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
	bkdLines = max(b.bkdEager-aboveScreen, b.height-onScreen)
	if b.followMode {
		// In follow mode it doesnt matter how many lines we return in fwdLines. We will always try reading more.
		fwdLines = 0
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
