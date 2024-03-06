package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/gdamore/tcell/v2"
)

type Application struct {
	// The input file handle
	inputReader *os.File

	// If true, continue reading from reader forwards
	followMode bool

	// The width of the terminal
	width int
	// The height of the terminal
	height int

	screen tcell.Screen
	buffer *Buffer
}

func NewApplication(inputReader *os.File, followMode bool) *Application {
	application := &Application{
		inputReader: inputReader,
		followMode:  followMode,
	}

	return application
}

func (a *Application) Run(ctx context.Context, cancelCtx context.CancelFunc) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("failed to create terminal screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("failed to initialize terminal screen: %w", err)
	}

	quit := func() {
		// You have to catch panics in a defer, clean up, and
		// re-raise them - otherwise your application can
		// die without leaving any diagnostic trace.
		maybePanic := recover()
		screen.Fini()
		if maybePanic != nil {
			panic(maybePanic)
		}
	}
	defer quit()

	a.width, a.height = screen.Size()

	buffer, err := NewBuffer(a.width, a.height, a.followMode, a.inputReader, ctx)
	if err != nil {
		return fmt.Errorf("failed to create buffer: %w", err)
	}
	a.buffer = buffer

	whence := io.SeekStart
	if a.followMode {
		whence = io.SeekEnd
	}

	if err := a.buffer.SeekAndPopulate(0, whence); err != nil {
		return fmt.Errorf("failed to populate the application buffer: %w", err)
	}

	screen.Clear()
	screen.SetContent(0, 0, 'H', nil, tcell.StyleDefault)
	screen.SetContent(1, 0, 'i', nil, tcell.StyleDefault)

	go func() {
		for {
			// Update screen
			screen.Show()

			// Poll event
			ev := screen.PollEvent()

			// Process event
			switch ev := ev.(type) {
			case *tcell.EventResize:
				screen.Sync()
			case *tcell.EventKey:
				if ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC || ev.Rune() == 'q' {
					cancelCtx()
					return
				}
			}
		}
	}()

	<-ctx.Done()
	return ctx.Err()
}
