package main

import "sync"

type BufferRecordList interface {
	Append(r *record)
	Prepend(r *record)
	PopFirst() *record
	PopLast() *record
	Clear()
	ScrollUp(lines int) int
	ScrollDown(lines int) int
	GetLinesToRender(lineCount int) []string
}

type bufferRecordList struct {
	mu   *sync.Mutex
	head *bufferRecord
	tail *bufferRecord

	// Pointer to the record that is currently at the top of the screen.
	screenTop *bufferRecord
	// A record may span multiple screen lines. This is the offset of the first
	// line within the record to render at the top of the screen.
	screenTopOffset int

	// If true, we're within a WithLock call. This will prevent the other
	// functions from attempting to lock the mutex.
	withinLock bool
}

type bufferRecord struct {
	record *record
	prev   *bufferRecord
	next   *bufferRecord
}

func NewBufferRecordList() *bufferRecordList {
	return &bufferRecordList{
		mu: &sync.Mutex{},
	}
}

func (l *bufferRecordList) WithLock(f func(*bufferRecordList) any) any {
	if l.withinLock {
		return f(l)
	}

	l.mu.Lock()
	defer func() {
		l.mu.Unlock()
	}()

	// Construct a new instance that will not perform locks.
	unlockedInst := &bufferRecordList{
		head:            l.head,
		tail:            l.tail,
		screenTop:       l.screenTop,
		screenTopOffset: l.screenTopOffset,
		withinLock:      true,
	}
	defer func() {
		// Assign back to the original instance.
		l.head, l.tail, l.screenTop, l.screenTopOffset = unlockedInst.head, unlockedInst.tail, unlockedInst.screenTop, unlockedInst.screenTopOffset
	}()

	return f(unlockedInst)
}

func (l *bufferRecordList) Append(r *record) {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	newRecord := &bufferRecord{record: r}
	if l.head == nil {
		l.head = newRecord
		l.tail = newRecord
	} else {
		l.tail.next = newRecord
		newRecord.prev = l.tail
		l.tail = newRecord
	}

	if l.screenTop == nil {
		l.screenTop = newRecord
		l.screenTopOffset = 0
	}
}

func (l *bufferRecordList) Prepend(r *record) {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	newRecord := &bufferRecord{record: r}
	if l.head == nil {
		l.head = newRecord
		l.tail = newRecord
	} else {
		l.head.prev = newRecord
		newRecord.next = l.head
		l.head = newRecord
	}

	if l.screenTop == nil {
		l.screenTop = newRecord
		l.screenTopOffset = 0
	}
}

func (l *bufferRecordList) PopFirst() *record {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	if l.head == nil {
		return nil
	}

	r := l.head.record

	next := l.head.next
	l.head = next

	if next == nil {
		l.tail = nil
	} else {
		next.prev = nil
	}

	return r
}

func (l *bufferRecordList) PopLast() *record {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	if l.tail == nil {
		return nil
	}

	r := l.tail.record

	prev := l.tail.prev
	l.tail = prev

	if prev == nil {
		l.head = nil
	} else {
		prev.next = nil
	}

	return r
}

func (l *bufferRecordList) Clear() {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	l.head = nil
	l.tail = nil
	l.screenTop = nil
	l.screenTopOffset = 0
}

func (l *bufferRecordList) ScrollUp(lines int) int {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	linesMoved := 0
	if l.screenTop == nil {
		return linesMoved
	}

	nextScreenTop := l.screenTop
	for {
		if l.screenTopOffset >= lines {
			linesMoved += lines
			l.screenTopOffset -= lines
			l.screenTop = nextScreenTop
			return linesMoved
		}

		if l.screenTopOffset > 0 {
			lines -= l.screenTopOffset
			linesMoved += l.screenTopOffset
			l.screenTopOffset = 0
		}

		if nextScreenTop.prev == nil {
			l.screenTop = nextScreenTop
			return linesMoved
		}

		nextScreenTop = nextScreenTop.prev
		l.screenTopOffset = len(nextScreenTop.record.lines) - 1
		lines--
		linesMoved++
	}
}

func (l *bufferRecordList) ScrollDown(lines int) int {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	linesMoved := 0
	if l.screenTop == nil {
		return linesMoved
	}

	nextScreenTop := l.screenTop
	for {
		linesLeftInRecord := len(nextScreenTop.record.lines) - l.screenTopOffset - 1
		if linesLeftInRecord >= lines {
			linesMoved += lines
			l.screenTopOffset += lines
			l.screenTop = nextScreenTop
			return linesMoved
		}

		if linesLeftInRecord > 0 {
			lines -= linesLeftInRecord
			linesMoved += linesLeftInRecord
			l.screenTopOffset += linesLeftInRecord
		}

		if nextScreenTop.next == nil {
			l.screenTop = nextScreenTop
			return linesMoved
		}

		nextScreenTop = nextScreenTop.next
		l.screenTopOffset = 0
		lines--
		linesMoved++
	}
}

func (l *bufferRecordList) GetLinesToRender(lineCount int) []string {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	result := make([]string, 0)

	offset := l.screenTopOffset
	for record := l.screenTop; record != nil; record = record.next {
		takeLines := len(record.record.lines) - offset
		if takeLines >= lineCount {
			result = append(result, record.record.lines[offset:offset+lineCount]...)
			break
		}

		result = append(result, record.record.lines[:takeLines]...)
		lineCount -= takeLines
		offset = 0
	}

	return result
}
