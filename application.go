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
	a.screen = screen

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

	go func() {
		defer cancelCtx()

		eventsCh := make(chan tcell.Event)
		quitCh := make(chan struct{})

		buffer.SetPostEventFunc(func(ev tcell.Event) error {
			return screen.PostEvent(ev)
		})

		go screen.ChannelEvents(eventsCh, quitCh)

		for {
			// Update screen
			screen.Show()

			// Get next event.
			ev := <-eventsCh
			if ev == nil {
				return
			}

			// Process event
			switch ev := ev.(type) {
			case *tcell.EventResize:
				screen.Sync()
			case *tcell.EventKey:
				needsRerender := false

				if ev.Rune() == 'q' {
					close(quitCh)
				} else {
					switch ev.Key() {
					case tcell.KeyUp:
						a.buffer.records.ScrollUp(1)
						needsRerender = true
					case tcell.KeyPgUp:
						a.buffer.records.ScrollUp(a.height)
						needsRerender = true
					case tcell.KeyDown:
						a.buffer.records.ScrollDown(1)
						needsRerender = true
					case tcell.KeyPgDn:
						a.buffer.records.ScrollDown(a.height)
						needsRerender = true
					case tcell.KeyEscape:
					case tcell.KeyCtrlC:
						close(quitCh)
					}
				}

				if needsRerender {
					screen.Clear()
					a.RenderLogLines(a.buffer.records.GetLinesToRender(a.height))
				}
			case *tcell.EventInterrupt:
				screen.Clear()
				a.RenderLogLines(a.buffer.records.GetLinesToRender(a.height))
			}
		}
	}()

	<-ctx.Done()
	return ctx.Err()
}

func (a *Application) RenderLogLines(lines []string) {
	var x, y int
	y = 0
	var state *stepState
	for _, line := range lines {
		x = 0
		state = nil
		for len(line) > 0 {
			var ch string
			ch, line, state = step(line, state)
			w := state.Width()

			for offset := w - 1; offset >= 0; offset-- {
				runes := []rune(ch)
				if offset == 0 {
					a.screen.SetContent(x+offset, y, runes[0], runes[1:], tcell.StyleDefault)
				} else {
					a.screen.SetContent(x+offset, y, ' ', nil, tcell.StyleDefault)
				}
			}

			x += w
		}
		y++
	}
}
