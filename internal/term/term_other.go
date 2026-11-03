//go:build !(linux || darwin || freebsd || openbsd || netbsd || dragonfly || windows)

package term

import (
	"io"
	"os"
)

func isTerminal(*os.File) bool { return false }

func raw(*os.File) (io.Reader, func(), error) { return nil, nil, ErrNotTerminal }
