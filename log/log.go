package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
)

type Logger struct {
	l       *log.Logger
	rawMode bool
}

var (
	crlfPrefixer = regexp.MustCompile(`(?:([^\r])\n|^\n)`)
)

var std = NewFromLogger(log.Default(), false)

// Default returns the standard logger used by the package-level output functions.
func Default() *Logger { return std }

func New(out io.Writer, prefix string, flag int, rawMode bool) *Logger {
	return NewFromLogger(log.New(out, prefix, flag), rawMode)
}

func NewFromLogger(l *log.Logger, rawMode bool) *Logger {
	return &Logger{l: l, rawMode: rawMode}
}

func (l *Logger) fixString(str string) string {
	if !l.rawMode {
		return str
	}

	s := crlfPrefixer.ReplaceAllString(str, "$1\r\n")
	if len(s) == 0 || s[len(s)-1] != '\n' {
		s += "\r\n"
	}
	return s
}

// RawMode returns the raw mode for the logger.
func (l *Logger) RawMode() bool {
	return l.rawMode
}

// SetRawMode sets the raw mode for the logger.
func (l *Logger) SetRawMode(rawMode bool) {
	l.rawMode = rawMode
}

// SetOutput sets the output destination for the logger.
func (l *Logger) SetOutput(w io.Writer) {
	l.l.SetOutput(w)
}

// Output writes the output for a logging event. The string s contains
// the text to print after the prefix specified by the flags of the
// Logger. A newline is appended if the last character of s is not
// already a newline. Calldepth is used to recover the PC and is
// provided for generality, although at the moment on all pre-defined
// paths it will be 2.
func (l *Logger) Output(calldepth int, s string) error {
	// Prefix any LF with CR if not already done.
	s = l.fixString(s)
	return l.l.Output(calldepth+1, s)
}

// Print calls l.Output to print to the logger.
// Arguments are handled in the manner of [fmt.Print].
func (l *Logger) Print(v ...any) {
	s := l.fixString(fmt.Sprint(v...))
	l.l.Output(2, s)
}

// Printf calls l.Output to print to the logger.
// Arguments are handled in the manner of [fmt.Printf].
func (l *Logger) Printf(format string, v ...any) {
	s := l.fixString(fmt.Sprintf(format, v...))
	l.l.Output(2, s)
}

// Println calls l.Output to print to the logger.
// Arguments are handled in the manner of [fmt.Println].
func (l *Logger) Println(v ...any) {
	s := l.fixString(fmt.Sprintln(v...))
	l.l.Output(2, s)
}

// Fatal is equivalent to l.Print() followed by a call to [os.Exit](1).
func (l *Logger) Fatal(v ...any) {
	l.Print(v...)
	os.Exit(1)
}

// Fatalf is equivalent to l.Printf() followed by a call to [os.Exit](1).
func (l *Logger) Fatalf(format string, v ...any) {
	l.Printf(format, v...)
	os.Exit(1)
}

// Fatalln is equivalent to l.Println() followed by a call to [os.Exit](1).
func (l *Logger) Fatalln(v ...any) {
	l.Println(v...)
	os.Exit(1)
}

// Panic is equivalent to l.Print() followed by a call to panic().
func (l *Logger) Panic(v ...any) {
	s := fmt.Sprint(v...)
	l.Output(2, s)
	panic(s)
}

// Panicf is equivalent to l.Printf() followed by a call to panic().
func (l *Logger) Panicf(format string, v ...any) {
	s := fmt.Sprintf(format, v...)
	l.Output(2, s)
	panic(s)
}

// Panicln is equivalent to l.Println() followed by a call to panic().
func (l *Logger) Panicln(v ...any) {
	s := fmt.Sprintln(v...)
	l.Output(2, s)
	panic(s)
}

// Flags returns the output flags for the logger.
// The flag bits are [Ldate], [Ltime], and so on.
func (l *Logger) Flags() int {
	return l.l.Flags()
}

// SetFlags sets the output flags for the logger.
// The flag bits are [Ldate], [Ltime], and so on.
func (l *Logger) SetFlags(flag int) {
	l.l.SetFlags(flag)
}

// Prefix returns the output prefix for the logger.
func (l *Logger) Prefix() string {
	return l.l.Prefix()
}

// SetPrefix sets the output prefix for the logger.
func (l *Logger) SetPrefix(prefix string) {
	s := l.fixString(prefix)
	l.l.SetPrefix(s)
}

// Writer returns the output destination for the logger.
func (l *Logger) Writer() io.Writer {
	return l.l.Writer()
}

// SetOutput sets the output destination for the standard logger.
func SetOutput(w io.Writer) {
	std.SetOutput(w)
}

// Flags returns the output flags for the standard logger.
// The flag bits are [Ldate], [Ltime], and so on.
func Flags() int {
	return std.Flags()
}

// SetFlags sets the output flags for the standard logger.
// The flag bits are [Ldate], [Ltime], and so on.
func SetFlags(flag int) {
	std.SetFlags(flag)
}

// Prefix returns the output prefix for the standard logger.
func Prefix() string {
	return std.Prefix()
}

// SetPrefix sets the output prefix for the standard logger.
func SetPrefix(prefix string) {
	std.SetPrefix(prefix)
}

// Writer returns the output destination for the standard logger.
func Writer() io.Writer {
	return std.Writer()
}

// These functions write to the standard logger.

// Print calls Output to print to the standard logger.
// Arguments are handled in the manner of [fmt.Print].
func Print(v ...any) {
	std.Print(v...)
}

// Printf calls Output to print to the standard logger.
// Arguments are handled in the manner of [fmt.Printf].
func Printf(format string, v ...any) {
	std.Printf(format, v...)
}

// Println calls Output to print to the standard logger.
// Arguments are handled in the manner of [fmt.Println].
func Println(v ...any) {
	std.Println(v...)
}

// Fatal is equivalent to [Print] followed by a call to [os.Exit](1).
func Fatal(v ...any) {
	std.Output(2, fmt.Sprint(v...))
	os.Exit(1)
}

// Fatalf is equivalent to [Printf] followed by a call to [os.Exit](1).
func Fatalf(format string, v ...any) {
	std.Fatalf(format, v...)
}

// Fatalln is equivalent to [Println] followed by a call to [os.Exit](1).
func Fatalln(v ...any) {
	std.Fatalln(v...)
}

// Panic is equivalent to [Print] followed by a call to panic().
func Panic(v ...any) {
	std.Panic(v...)
}

// Panicf is equivalent to [Printf] followed by a call to panic().
func Panicf(format string, v ...any) {
	std.Panicf(format, v...)
}

// Panicln is equivalent to [Println] followed by a call to panic().
func Panicln(v ...any) {
	std.Panicln(v...)
}

// Output writes the output for a logging event. The string s contains
// the text to print after the prefix specified by the flags of the
// Logger. A newline is appended if the last character of s is not
// already a newline. Calldepth is the count of the number of
// frames to skip when computing the file name and line number
// if [Llongfile] or [Lshortfile] is set; a value of 1 will print the details
// for the caller of Output.
func Output(calldepth int, s string) error {
	return std.Output(calldepth+1, s) // +1 for this frame.
}
