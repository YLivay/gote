package utils

import (
	"strings"

	"github.com/rivo/uniseg"
)

// stepState is based off of rivo/tview's strings.go:stepState struct without
// the styling and tag parsing logic.
// https://github.com/rivo/tview/blob/8a0aeb0aa377d2009202dc3111f17f13cd9f22ce/strings.go
type stepState struct {
	unisegState int
	boundaries  int
	grossLength int
}

// LineBreak returns whether the string can be broken into the next line after
// the returned grapheme cluster. If optional is true, the line break is
// optional. If false, the line break is mandatory, e.g. after a newline
// character.
func (s *stepState) LineBreak() (lineBreak, optional bool) {
	switch s.boundaries & uniseg.MaskLine {
	case uniseg.LineCanBreak:
		return true, true
	case uniseg.LineMustBreak:
		return true, false
	}
	return false, false // uniseg.LineDontBreak.
}

// Width returns the grapheme cluster's width in cells.
func (s *stepState) Width() int {
	return s.boundaries >> uniseg.ShiftWidth
}

// GrossLength returns the grapheme cluster's length in bytes, including any
// tags that were parsed but not explicitly returned.
func (s *stepState) GrossLength() int {
	return s.grossLength
}

// step is based off of rivo/tview's strings.go:step function without the
// styling and tag parsing logic.
// https://github.com/rivo/tview/blob/8a0aeb0aa377d2009202dc3111f17f13cd9f22ce/strings.go
func step(str string, state *stepState) (cluster, rest string, newState *stepState) {
	// Set up initial state.
	if state == nil {
		state = &stepState{
			unisegState: -1,
		}
	}
	if len(str) == 0 {
		newState = state
		return
	}

	// Get a grapheme cluster.
	preState := state.unisegState
	cluster, rest, state.boundaries, state.unisegState = uniseg.StepString(str, preState)
	state.grossLength = len(cluster)
	if rest == "" {
		if !uniseg.HasTrailingLineBreakInString(cluster) {
			state.boundaries &^= uniseg.MaskLine
		}
	}

	newState = state
	return
}

// WordWrap is based off rivo/tview's strings.go:WordWrap function without the
// styling and tag parsing logic.
// https://github.com/rivo/tview/blob/8a0aeb0aa377d2009202dc3111f17f13cd9f22ce/strings.go
func WordWrap(text string, width int) (lines []string) {
	if width <= 0 {
		return
	}

	var (
		state                                              *stepState
		lineWidth, lineLength, lastOption, lastOptionWidth int
	)
	str := text
	for len(str) > 0 {
		// Parse the next character.
		_, str, state = step(str, state)
		cWidth := state.Width()

		// Would it exceed the line width?
		if lineWidth+cWidth > width {
			if lastOptionWidth == 0 {
				// No split point so far. Just split at the current position.
				lines = append(lines, text[:lineLength])
				text = text[lineLength:]
				lineWidth, lineLength, lastOption, lastOptionWidth = 0, 0, 0, 0
			} else {
				// Split at the last split point.
				lines = append(lines, text[:lastOption])
				text = text[lastOption:]
				lineWidth -= lastOptionWidth
				lineLength -= lastOption
				lastOption, lastOptionWidth = 0, 0
			}
		}

		// Move ahead.
		lineWidth += cWidth
		lineLength += state.GrossLength()

		// Check for split points.
		if lineBreak, optional := state.LineBreak(); lineBreak {
			if optional {
				// Remember this split point.
				lastOption = lineLength
				lastOptionWidth = lineWidth
			} else {
				// We must split here.
				lines = append(lines, strings.TrimRight(text[:lineLength], "\n\r"))
				text = text[lineLength:]
				lineWidth, lineLength, lastOption, lastOptionWidth = 0, 0, 0, 0
			}
		}
	}
	lines = append(lines, text)

	return
}
