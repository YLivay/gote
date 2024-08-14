package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/YLivay/gote/reader"
	"github.com/gdamore/tcell/v2"
	"github.com/itchyny/gojq"
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

	// A function that triggers the async readers to reevaluate how many lines
	// they need to read in each direction and continue reading if necessary.
	continueAsyncReads func()

	// The managed list of records loaded by this buffer's scanners.
	records *bufferRecordList

	// A compiled jq expression that will be applied to the lines read from the input file.
	jqExpr *gojq.Code

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

	// A logger to use.
	logger *log.Logger
}

func NewBuffer(width, height int, followMode bool, inputReader *os.File, ctx context.Context) (*Buffer, error) {
	inputFname := inputReader.Name()

	fwdReader := inputReader

	logfile, err := os.OpenFile("logfile", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	context.AfterFunc(ctx, func() {
		logfile.Close()
	})

	bkdReader, err := os.Open(inputFname)
	if err != nil {
		return nil, err
	}

	jqQuery, err := gojq.Parse(". | .time /= 1000 | .time |= todateiso8601 | select(.name | test(\"Pelecard\")) | {time, name, msg}")
	if err != nil {
		return nil, err
	}
	jqExpr, err := gojq.Compile(jqQuery)
	if err != nil {
		return nil, err
	}

	buffer := &Buffer{
		mu:                 &sync.Mutex{},
		ctx:                ctx,
		width:              width,
		height:             height,
		followMode:         followMode,
		fwdReader:          fwdReader,
		bkdReader:          bkdReader,
		bkdEager:           height * 2,
		fwdEager:           height * 2,
		continueAsyncReads: func() {},
		records:            NewBufferRecordList(),
		jqExpr:             jqExpr,
		postEvent: func(e tcell.Event) error {
			return nil
		},
		muCancelPopulate: &sync.Mutex{},
		cancelPopulate: func(err error) <-chan any {
			ch := make(chan any)
			close(ch)
			return ch
		},
		logger: log.New(logfile, "", log.Ltime|log.Lmicroseconds),
	}

	// buffer.setupAsyncReads(nil)

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

	b.records.Clear()

	b.mu.Unlock()

	b.setupAsyncReads(errors.New("changing seek position"))

	return nil
}

// Scroll scrolls the buffer by the given number of lines. A positive number
// scrolls down, a negative number scrolls up.
//
// Returns the number of lines actually moved. If scrolling down the value will
// be positive or zero, if scrolling up the value will be negative or zero.
func (b *Buffer) Scroll(lines int) int {
	b.logger.Println("[buffer.Scroll] scrolling buffer by", lines, "lines")

	if lines == 0 {
		return 0
	}

	var linesMoved int
	b.records.WithLock(func(records *bufferRecordList) any {
		b.logger.Println("[buffer.Scroll] current record status: linesAboveScreenTop =", records.linesAboveScreenTop, ", linesBelowScreenTop =", records.linesBelowScreenTop, ", screenTopOffset =", records.screenTopOffset)
		if lines > 0 {
			linesMoved = records.ScrollDown(lines)
		} else {
			linesMoved = -records.ScrollUp(-lines)
		}
		b.logger.Println("[buffer.Scroll] scrolled buffer by", linesMoved, "lines")
		b.logger.Println("[buffer.Scroll] after scrolling record status: linesAboveScreenTop =", records.linesAboveScreenTop, ", linesBelowScreenTop =", records.linesBelowScreenTop, ", screenTopOffset =", records.screenTopOffset)
		return true
	})

	b.continueAsyncReads()

	return linesMoved
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
	b.muCancelPopulate.Lock()
	defer b.muCancelPopulate.Unlock()

	// When both readers are done and the continue channel has been disposed we
	// can consider this operation as done.
	bkdReaderDone := make(chan any)
	fwdReaderDone := make(chan any)
	continueMu := &sync.RWMutex{}
	continueCh := make(chan any)
	continueDone := false
	doneCh := make(chan any)
	go func() {
		<-bkdReaderDone
		<-fwdReaderDone
		<-continueCh
		close(doneCh)
	}()

	// Used to signal the current populate process to abort.
	innerCtx, innerCancel := context.WithCancelCause(b.ctx)

	// Wrap innerCancel with a function that allows the caller to await the
	// populate process finishing.
	cancelPopulate := func(err error) <-chan any {
		// Generate a short 8 character hex string
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err != nil {
			panic(err)
		}
		prefix := fmt.Sprintf("[buffer.cancelPopulate %x]", buf[:])

		// log which function called cancelPopulate
		pc, _, lineNo, ok := runtime.Caller(1)
		if ok {
			funcName := runtime.FuncForPC(pc).Name()
			b.logger.Printf("%s called by %s:%d\n", prefix, funcName, lineNo)
		} else {
			b.logger.Println(prefix, "called by unknown")
		}

		innerCancel(err)
		go func() {
			b.logger.Println(prefix, "acquiring continueMu")
			continueMu.Lock()
			b.logger.Println(prefix, "acquired continueMu")
			if !continueDone {
				b.logger.Println(prefix, "closing continueCh")
				close(continueCh)
				continueDone = true
			} else {
				b.logger.Println(prefix, "continueCh already closed")
			}
			b.logger.Println(prefix, "releasing continueMu")
			continueMu.Unlock()
			b.logger.Println(prefix, "released continueMu")
		}()
		return doneCh
	}

	oldCancelPopulate := b.cancelPopulate
	b.cancelPopulate = cancelPopulate
	b.logger.Println("[buffer.setupAsyncReads] waiting for old populate process to finish")
	<-oldCancelPopulate(restartReason)
	b.logger.Println("[buffer.setupAsyncReads] old populate process finished")

	var bkdToRead, fwdToRead int
	var followMode bool

	b.continueAsyncReads = func() {
		// Generate a short 8 character hex string
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err != nil {
			panic(err)
		}
		prefix := fmt.Sprintf("[buffer.continueAsyncReads %x]", buf[:])

		// log which function called cancelPopulate
		pc, _, lineNo, ok := runtime.Caller(1)
		if ok {
			funcName := runtime.FuncForPC(pc).Name()
			b.logger.Printf("%s called by %s:%d\n", prefix, funcName, lineNo)
		} else {
			b.logger.Println(prefix, "called by unknown")
		}

		go func() {
			if innerCtx.Err() != nil {
				b.logger.Println(prefix, "skipping because innerCtx is canceled")
				return
			}

			b.logger.Println(prefix, "acquiring buffer lock")
			b.mu.Lock()
			b.logger.Println(prefix, "acquired buffer lock.")
			b.logger.Println(prefix, "calculating lines to read.")
			bkdToRead, fwdToRead = b.calcLinesToReadUsingRecords(b.records)
			followMode = b.followMode
			b.logger.Println(prefix, "calculated lines to read (bkdToRead =", bkdToRead, ", fwdToRead =", fwdToRead, ").")
			b.logger.Println(prefix, "releasing buffer lock.")
			b.mu.Unlock()
			b.logger.Println(prefix, "released buffer lock.")

			b.logger.Println(prefix, "acquiring continueMu")
			continueMu.Lock()
			b.logger.Println(prefix, "acquired continueMu.")
			if !continueDone {
				b.logger.Println(prefix, "closing continueCh and opening a new one.")
				close(continueCh)
				continueCh = make(chan any)
			} else {
				b.logger.Println(prefix, "not closing continueCh because continueDone = true.")
			}
			b.logger.Println(prefix, "releasing continueMu.")
			continueMu.Unlock()
			b.logger.Println(prefix, "released continueMu.")
		}()
	}

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

	bkdScanner, fwdScanner := b.bkdScanner, b.fwdScanner
	width, height := b.width, b.height
	bkdToRead, fwdToRead = b.calcLinesToReadUsingRecords(b.records)
	followMode = b.followMode

	firstBkdRead := true
	firstFwdRead := true

	b.logger.Println("[buffer.setupAsyncReads] starting readers loop (bkdToRead =", bkdToRead, ", fwdToRead =", fwdToRead, ")")

	go func() {
		defer close(bkdReaderDone)

		myContinueCh := continueCh
		var myBkdToRead int
		for {
			if firstBkdRead {
				firstBkdRead = false
			} else {
				b.logger.Println("[buffer.bkdReadLoop] waiting for continueCh")
				<-myContinueCh
				b.logger.Println("[buffer.bkdReadLoop] got continueCh")
			}

			if innerCtx.Err() != nil {
				b.logger.Println("[buffer.bkdReadLoop] innerCtx is canceled, stopping")
				return
			}

			b.logger.Println("[buffer.bkdReadLoop] acquiring continueMu for reading")
			continueMu.RLock()
			b.logger.Println("[buffer.bkdReadLoop] acquired continueMu for reading")
			myContinueCh = continueCh
			myBkdToRead = bkdToRead
			b.logger.Println("[buffer.bkdReadLoop] will try reading", myBkdToRead, "lines")
			b.logger.Println("[buffer.bkdReadLoop] releasing continueMu for reading")
			continueMu.RUnlock()
			b.logger.Println("[buffer.bkdReadLoop] released continueMu for reading")

			for i := 0; i < myBkdToRead; i++ {
				b.logger.Println("[buffer.bkdReadLoop] loop", i+1, "of", myBkdToRead)
				if innerCtx.Err() != nil {
					b.logger.Println("[buffer.bkdReadLoop] innerCtx is canceled, stopping")
					return
				}

				b.logger.Println("[buffer.bkdReadLoop] reading line")
				line, pos, err := bkdScanner.ReadLine()
				if err != nil && !errors.Is(err, io.EOF) {
					b.logger.Println("[buffer.bkdReadLoop] failed to read line:", err.Error())
					panic(fmt.Errorf("failed to populate buffer (backwards read): %w", err))
				}
				b.logger.Println("[buffer.bkdReadLoop] read line:", string(line))

				// When EOF is returned with an empty line it doesnt necessarily
				// mean that an empty line exists at the start of the file. More
				// likely it means we didn't read anything, so avoid adding this
				// line to the buffer.
				if len(line) == 0 && errors.Is(err, io.EOF) {
					b.logger.Println("[buffer.bkdReadLoop] EOF with empty line, stopping.")
					return
				}

				b.records.WithLock(func(records *bufferRecordList) any {
					b.logger.Println("[buffer.bkdReadLoop] running with buffer records lock")
					r := b.parseLine(pos, line, width)
					if r == nil {
						myBkdToRead++
						return false
					}

					b.logger.Println("[buffer.bkdReadLoop] created record spanning", len(r.lines), "lines")
					b.logger.Println("[buffer.bkdReadLoop] current record status: linesAboveScreenTop =", records.linesAboveScreenTop, ", linesBelowScreenTop =", records.linesBelowScreenTop, ", screenTopOffset =", records.screenTopOffset)
					records.Prepend(r)
					b.logger.Println("[buffer.bkdReadLoop] after prepending record status: linesAboveScreenTop =", records.linesAboveScreenTop, ", linesBelowScreenTop =", records.linesBelowScreenTop, ", screenTopOffset =", records.screenTopOffset)

					// If prepending but we don't have a full screen of lines yet,
					// we should scroll up to try and fit more lines on screen.
					_, onScreen, _ := records.CalcScreenLines(height)
					canScroll := min(height-onScreen, len(r.lines))
					if canScroll > 0 {
						b.logger.Println("[buffer.bkdReadLoop] scrolling up", canScroll, "lines")
						records.ScrollUp(canScroll)
						b.logger.Println("[buffer.bkdReadLoop] after scrolling up. linesAboveScreenTop =", records.linesAboveScreenTop, ", linesBelowScreenTop =", records.linesBelowScreenTop, ", screenTopOffset =", records.screenTopOffset)
						b.continueAsyncReads()
					}

					return true
				})
				b.postEvent(tcell.NewEventInterrupt(nil))

				if errors.Is(err, io.EOF) {
					b.logger.Println("[buffer.bkdReadLoop] EOF, stopping")
					return
				}
			}
		}
	}()

	go func() {
		defer close(fwdReaderDone)

		myContinueCh := continueCh
		var myFwdToRead int
		for {
			if firstFwdRead {
				firstFwdRead = false
			} else {
				b.logger.Println("[buffer.fwdReadLoop] waiting for continueCh")
				<-myContinueCh
				b.logger.Println("[buffer.fwdReadLoop] got continueCh")
			}

			if innerCtx.Err() != nil {
				b.logger.Println("[buffer.fwdReadLoop] innerCtx is canceled, stopping")
				return
			}

			b.logger.Println("[buffer.fwdReadLoop] acquiring continueMu for reading")
			continueMu.RLock()
			b.logger.Println("[buffer.fwdReadLoop] acquired continueMu for reading")
			myContinueCh = continueCh
			myFwdToRead = fwdToRead
			b.logger.Println("[buffer.fwdReadLoop] will try reading", myFwdToRead, "lines")
			b.logger.Println("[buffer.fwdReadLoop] releasing continueMu for reading")
			continueMu.RUnlock()
			b.logger.Println("[buffer.fwdReadLoop] released continueMu for reading")

			for i := 0; i < myFwdToRead || followMode; i++ {
				b.logger.Println("[buffer.fwdReadLoop] loop", i+1, "of", myFwdToRead)
				if innerCtx.Err() != nil {
					b.logger.Println("[buffer.fwdReadLoop] innerCtx is canceled, stopping")
					return
				}

				b.logger.Println("[buffer.fwdReadLoop] reading line")
				if !fwdScanner.Scan() {
					if err := fwdScanner.Err(); err != nil {
						b.logger.Println("[buffer.fwdReadLoop] failed to read line:", err.Error())
						panic(fmt.Errorf("failed to populate buffer (forwards read): %w", err))
					}

					if followMode {
						// If EOF, but we're in follow mode, wait a bit and try
						// reading the file again.
						b.logger.Println("[buffer.fwdReadLoop] EOF in follow mode, waiting a bit and trying again")
						<-time.After(1 * time.Second)
						continue
					} else {
						// If EOF and we're not in follow mode, stop. we have
						// all the data we wanted.
						b.logger.Println("[buffer.fwdReadLoop] EOF and not in follow mode, stopping")
						return
					}
				}

				line := fwdScanner.Bytes()
				b.logger.Println("[buffer.fwdReadLoop] read line:", string(line))

				b.records.WithLock(func(records *bufferRecordList) any {
					b.logger.Println("[buffer.fwdReadLoop] running with buffer records lock")
					r := b.parseLine(-1, line, width)
					if r == nil {
						myFwdToRead++
						return false
					}

					b.logger.Println("[buffer.fwdReadLoop] created record spanning", len(r.lines), "lines")
					b.logger.Println("[buffer.fwdReadLoop] current record status: linesAboveScreenTop =", records.linesAboveScreenTop, ", linesBelowScreenTop =", records.linesBelowScreenTop, ", screenTopOffset =", records.screenTopOffset)
					records.Append(r)
					b.logger.Println("[buffer.fwdReadLoop] after appending record status: linesAboveScreenTop =", records.linesAboveScreenTop, ", linesBelowScreenTop =", records.linesBelowScreenTop, ", screenTopOffset =", records.screenTopOffset)

					if followMode {
						b.logger.Println("[buffer.fwdReadLoop] scrolling to bottom")
						records.ScrollToBottom(height)
						b.logger.Println("[buffer.fwdReadLoop] after scrolling to bottom. linesAboveScreenTop =", records.linesAboveScreenTop, ", linesBelowScreenTop =", records.linesBelowScreenTop, ", screenTopOffset =", records.screenTopOffset)
						b.continueAsyncReads()
					}
					return true
				})
				b.postEvent(tcell.NewEventInterrupt(nil))
			}
		}
	}()
}

func (b *Buffer) parseLine(pos int64, line []byte, width int) *record {
	var data any
	if err := json.Unmarshal(line, &data); err != nil {
		return nil
	}

	var parsed map[string]any
	var ok bool
	if parsed, ok = data.(map[string]any); !ok {
		return nil
	}

	jqIter := b.jqExpr.Run(parsed)
	result, ok := jqIter.Next()
	if !ok {
		return nil
	}
	if _err, ok := result.(error); ok {
		b.logger.Println("[buffer.parseLine] jq error:", _err.Error())
		return nil
	}

	newLine, err := json.Marshal(result)
	if err != nil {
		return nil
	}

	return newRecord(pos, newLine, width)
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
