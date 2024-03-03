package main

import (
	"bufio"
	"errors"
	"os"

	"github.com/YLivay/gote/log"
	"github.com/mattn/go-tty"
	"golang.org/x/term"
)

type TTY interface {
	ReadRune() (rune, error)
}

type stdinTTY struct {
	TTY
	reader *bufio.Reader
}

func (s *stdinTTY) ReadRune() (rune, error) {
	rune, _, err := s.reader.ReadRune()
	return rune, err
}

func ensureTty() (ttyReader TTY, cleanupFn func() error, err error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		log.Println("Reopening /dev/tty for user input")
		newTty, err := tty.Open()
		if err != nil {
			return nil, nil, errors.New("Failed to open /dev/tty: " + err.Error())
		}

		ttyReader = newTty
		cleanupFn = func() error { return newTty.Close() }
	} else {
		// Make os.Stdin raw. This is necessary to read single characters from
		// the terminal without the need to press enter
		oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return nil, nil, errors.New("Failed to make os.Stdin raw: " + err.Error())
		}

		oldRawMode := log.Default().RawMode()
		log.Default().SetRawMode(true)
		ttyReader = &stdinTTY{reader: bufio.NewReader(os.Stdin)}
		cleanupFn = func() error {
			log.Default().SetRawMode(oldRawMode)
			return term.Restore(int(os.Stdin.Fd()), oldState)
		}
	}

	return ttyReader, cleanupFn, nil
}

func getSize() (int, int, error) {
	width, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 0, 0, errors.New("Failed to get terminal size: " + err.Error())
	}

	return width, height, nil
}
