package main

import "sync"

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

// Append adds a record to the end of the list.
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

// Prepend adds a record to the end of the list.
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

// PopFirst removes the first record from the list and returns it.
//
// If the screen top is the same as the record being removed, the screen top is
// moved to the next record and the screen top offset is reset to 0.
func (l *bufferRecordList) PopFirst() *record {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	head := l.head
	if head == nil {
		return nil
	}

	next := head.next
	l.head = next

	if next == nil {
		l.tail = nil
		l.screenTop = nil
		l.screenTopOffset = 0
	} else {
		if l.screenTop == head {
			l.screenTop = next
			l.screenTopOffset = 0
		}
		next.prev = nil
	}

	return head.record
}

// PopLast removes the last record from the list and returns it.
//
// If the screen top is the same as the record being removed, the screen top is
// moved to the previous record and the screen top offset is reset to 0.
func (l *bufferRecordList) PopLast() *record {
	if !l.withinLock {
		l.mu.Lock()
		defer l.mu.Unlock()
	}

	tail := l.tail
	if tail == nil {
		return nil
	}

	prev := tail.prev
	l.tail = prev

	if prev == nil {
		l.head = nil
		l.screenTop = nil
		l.screenTopOffset = 0
	} else {
		if l.screenTop == tail {
			l.screenTop = prev
			l.screenTopOffset = 0
		}
		prev.next = nil
	}

	return tail.record
}

// Clear clears all the records from this list and resets the screen top and
// screen top offset.
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

// ScrollUp attempts to move the screen top up by the given number of lines.
//
// Returns the number of lines actually moved.
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

// ScrollDown attempts to move the screen top down by the given number of lines.
//
// Returns the number of lines actually moved.
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

// GetLinesToRender returns the lines to render on the screen starting from screen top and screen top offset.
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
