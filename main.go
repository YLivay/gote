package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/YLivay/gote/log"
)

func main() {
	if err := os.WriteFile("test.txt", []byte("Hello\nhi!"), 0644); err != nil {
		log.Fatalln("Failed to write test file:", err)
	}

	f, err := os.Open("test.txt")
	if err != nil {
		log.Fatalln("Failed to open test file:", err)
	}

	reader, err := NewBackwardsLineScanner(f, 5)
	if err != nil {
		log.Fatalln("Failed to initialize backwards scanner:", err)
	}

	line, err := reader.ReadLine()
	if err != nil {
		log.Fatalln("Failed to read line:", err)
	}
	log.Println("Read line:", string(line))

	// err := run()
	// if err != nil {
	// 	log.Fatalln(err.Error())
	// }

	// // Nothing to do
	// log.Println("All done")
}

func run() error {
	ctx, cancelCtx := context.WithCancel(context.Background())

	cleanupOsSignals := setupOsSignals(ctx, cancelCtx)
	defer cleanupOsSignals()

	filename := "-"
	reader, cleanupReader, err := prepareReader(filename)
	if err != nil {
		return errors.New("Failed to prepare reader: " + err.Error())
	}
	defer cleanupReader()

	// Check if os.Stdin is a tty. If it isn't, we need to initialize a new one for user input.
	tty, cleanupTty, err := ensureTty()
	if err != nil {
		return errors.New("Failed to ensure tty: " + err.Error())
	}
	defer cleanupTty()

	go func() {
		// Read keys from the tty and send them to the program
		for {
			r, err := tty.ReadRune()
			if err != nil {
				log.Println("Failed to read from /dev/tty:", err)
				return
			}
			log.Println("Read rune:", r, string(r))

			switch r {
			case 'q':
				cancelCtx()
				return
			}
		}
	}()

	// p := tea.NewProgram(AppState{reader: reader}, tea.WithContext(ctx))
	// if _, err := p.Run(); err != nil {
	// 	log.Fatalln(err.Error())
	// }

	// Sleep for a bit
	select {
	case <-time.After(30 * time.Second):
	case <-ctx.Done():
		log.Println("Sleep interrupted")
	}

	b := make([]byte, 10)
	reader.Seek(0, io.SeekStart)
	_, err = reader.Read(b)
	if err != nil {
		log.Println("Failed to read file:", err)
	}
	log.Println(string(b))
	return nil
}

func setupOsSignals(ctx context.Context, cancelCtx context.CancelFunc) (cleanup func()) {
	// Catch ctrl+c signal and make it close the context instead of immediately
	// exiting. This allows us to do some cleanup.
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt)

	cleanup = func() {
		signal.Stop(signalChan)
		cancelCtx()
	}

	go func() {
		select {
		case <-signalChan:
			log.Println("Ctrl+C pressed")
			cancelCtx()
		case <-ctx.Done():
		}
	}()

	return cleanup
}

func prepareReader(filename string) (reader *os.File, cleanup func(), err error) {
	// As resources are created in this function, accumulate functions to clean
	// them up in this slice.
	var deferredCleanups []func()
	cleanup = func() {
		// Invoke deferredCleanups in reverse order.
		for i := len(deferredCleanups) - 1; i >= 0; i-- {
			deferredCleanups[i]()
		}
	}

	if filename == "-" {
		reader = os.Stdin
	} else {
		reader, err = os.Open(filename)
		if err != nil {
			return nil, nil, errors.New("Failed to open file for reading: " + err.Error())
		}

		fileToClose := reader
		deferredCleanups = append(deferredCleanups, func() { fileToClose.Close() })
	}

	// Test if the file is seekable without changing the current position
	_, err = reader.Seek(0, io.SeekCurrent)
	if err != nil {
		// If the file is not seekable we need to pipe it through a temporary file first.
		// This is the case for stdin or other special files like sockets or pipes.
		log.Println("Input is not seekable, piping through a temporary file")
		tempWriter, err := os.CreateTemp("", "gote.tmp")
		if err != nil {
			cleanup()
			return nil, nil, errors.New("Failed to create temporary file: " + err.Error())
		}

		tempFname := tempWriter.Name()
		log.Println("Using temporary file:", tempFname)

		// Pipe the input to the temporary file asyncronously
		go func(tempWriter *os.File, pipeReader *os.File) {
			_, copyErr := io.Copy(tempWriter, pipeReader)
			if copyErr != nil {
				log.Println("Failed to copy input to temporary file:", copyErr)
			}

			// Attempt to close the temp writer.
			closeErr := tempWriter.Close()
			alreadyClosed := closeErr != nil && strings.HasSuffix(closeErr.Error(), "file already closed")
			closeErrIsUnexpected := closeErr != nil && !alreadyClosed

			// Log unexpected errors.
			if closeErrIsUnexpected {
				log.Println("Failed to close temporary file, it might not get deleted properly:", closeErr)
			}

			if (copyErr == nil || copyErr == io.EOF) && (closeErr == nil || alreadyClosed) {
				log.Println("Input closed")
			}
		}(tempWriter, reader)

		// Open the new tempfile again for reading.
		reader, err = os.Open(tempFname)
		if err != nil {
			cleanup()
			return nil, nil, errors.New("Failed to open temporary file for reading: " + err.Error())
		}

		deferredCleanups = append(deferredCleanups, func() {
			log.Println("Disposing temporary file:", tempFname)

			if err := tempWriter.Close(); err != nil {
				if !strings.HasSuffix(err.Error(), "file already closed") {
					log.Println("Failed to close the writer end of the temporary file:", err)
				}
			}

			if err := reader.Close(); err != nil {
				if !strings.HasSuffix(err.Error(), "file already closed") {
					log.Println("Failed to close the reader end of the temporary file:", err)
				}
			}

			if err := os.Remove(tempFname); err != nil {
				if !os.IsNotExist(err) {
					log.Println("Failed to remove temporary file:", err)
				}
			}
		})
	}

	return reader, cleanup, nil
}
