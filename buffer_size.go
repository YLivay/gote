package main

type BufferSizeNegotiator struct {
	application  *Application
	linesBack    int
	linesForward int
}

func NewBufferSizeNegotiator(application *Application) *BufferSizeNegotiator {
	return &BufferSizeNegotiator{
		application: application,
	}
}

func (b *BufferSizeNegotiator) Negotiate() (bkdLines, fwdLines int) {
	buffer := b.application.buffer

	unlock := buffer.setLocks()
	defer unlock()

	// Figure out how many lines we have above, below and on the screen.
	aboveScreen, onScreen, belowScreen := b.calcScreenLines()

	bkdLines = max(buffer.bkdEager-aboveScreen, 0)
	if b.application.followMode {
		// In follow mode we are interested in reading all available input as fast
		// as possible below the screen.
		fwdLines = 1
	} else {
		// In non-follow mode we are interested in reading ahead of both the top and
		// bottom of the screen.
		fwdLines = b.application.height - onScreen + max(buffer.fwdEager-belowScreen, 0)
	}

	b.linesBack = bkdLines
	b.linesForward = fwdLines
	return
}

func (b *BufferSizeNegotiator) Prune() {
	buffer := b.application.buffer

	unlock := buffer.setLocks()
	defer unlock()

	aboveScreen, _, belowScreen := b.calcScreenLines()

	// Prune the buffer to the desired size.
	records := buffer.records
	recordLines := len(records.head.record.lines)
	for aboveScreen-recordLines > b.linesBack {
		records.PopFirst()
		aboveScreen -= recordLines
		recordLines = len(records.head.record.lines)
	}

	// Only prune forward buffer if we are not in follow mode.
	if !b.application.followMode {
		recordLines = len(records.tail.record.lines)
		for belowScreen-recordLines > b.linesForward {
			records.PopLast()
			belowScreen -= recordLines
			recordLines = len(records.tail.record.lines)
		}
	}
}

// calcScreenLines figures out how many lines we have above, on, and below the
// screen. It expects the buffer's forward and backwards readers to be locked.
func (b *BufferSizeNegotiator) calcScreenLines() (aboveScreen, onScreen, belowScreen int) {
	buffer := b.application.buffer
	screenTop := buffer.records.screenTop
	if screenTop == nil {
		return 0, 0, 0
	}

	aboveScreen += buffer.records.screenTopOffset
	for r := screenTop.prev; r != nil; r = r.prev {
		aboveScreen += len(r.record.lines)
	}
	belowScreen += len(screenTop.record.lines) - buffer.records.screenTopOffset
	for r := screenTop.next; r != nil; r = r.next {
		belowScreen += len(r.record.lines)
	}
	onScreen = min(belowScreen, b.application.height)
	belowScreen -= onScreen
	return
}
