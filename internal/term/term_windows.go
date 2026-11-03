//go:build windows

package term

import (
	"io"
	"os"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
	procReadConsoleW   = kernel32.NewProc("ReadConsoleW")
)

const (
	enableProcessedInput = 0x0001
	enableLineInput      = 0x0002
	enableEchoInput      = 0x0004
	enableWindowInput    = 0x0008
)

func isTerminal(f *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil
}

func setConsoleMode(h syscall.Handle, mode uint32) error {
	r, _, err := procSetConsoleMode.Call(uintptr(h), uintptr(mode))
	if r == 0 {
		return err
	}
	return nil
}

func raw(f *os.File) (io.Reader, func(), error) {
	h := syscall.Handle(f.Fd())
	var old uint32
	if err := syscall.GetConsoleMode(h, &old); err != nil {
		return nil, nil, ErrNotTerminal
	}
	// libuv's raw mode: no line input, no echo, no processing of Ctrl+C.
	if err := setConsoleMode(h, enableWindowInput); err != nil {
		return nil, nil, err
	}
	return &consoleReader{h: h}, func() { _ = setConsoleMode(h, old) }, nil
}

// consoleReader reads the console's UTF-16 characters (ReadConsoleW) and gives them as UTF-8.
type consoleReader struct {
	h       syscall.Handle
	pending []byte
	high    uint16
}

func (c *consoleReader) Read(p []byte) (int, error) {
	for len(c.pending) == 0 {
		var buf [64]uint16
		var n uint32
		r, _, err := procReadConsoleW.Call(uintptr(c.h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)),
			uintptr(unsafe.Pointer(&n)), 0)
		if r == 0 {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}
		units := buf[:n]
		if c.high != 0 {
			units = append([]uint16{c.high}, units...)
			c.high = 0
		}
		if last := units[len(units)-1]; last >= 0xD800 && last <= 0xDBFF {
			c.high = last
			units = units[:len(units)-1]
		}
		for _, r := range utf16.Decode(units) {
			c.pending = utf8.AppendRune(c.pending, r)
		}
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}
