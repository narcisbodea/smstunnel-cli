// Package term tells whether standard input is a terminal and reads it key by key (raw mode), with the standard
// library only: termios on Linux, macOS and the BSDs, the console API on Windows.
package term

import (
	"errors"
	"io"
	"os"
)

// ErrNotTerminal is returned by Raw when f is not a terminal.
var ErrNotTerminal = errors.New("not a terminal")

// IsTerminal reports whether f is a terminal (a console on Windows).
func IsTerminal(f *os.File) bool { return isTerminal(f) }

// Raw switches the terminal f to raw mode (no echo, no line editing, Ctrl+C and Ctrl+D read as characters), the
// mode Node.js's setRawMode(true) sets. It returns a reader of the keys typed, as UTF-8, and a function that
// restores the terminal.
func Raw(f *os.File) (io.Reader, func(), error) { return raw(f) }
