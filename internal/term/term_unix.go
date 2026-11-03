//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package term

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

func getTermios(fd uintptr) (*syscall.Termios, error) {
	var t syscall.Termios
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlGetTermios, uintptr(unsafe.Pointer(&t))); errno != 0 {
		return nil, errno
	}
	return &t, nil
}

func setTermios(fd uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlSetTermios, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}

func isTerminal(f *os.File) bool {
	_, err := getTermios(f.Fd())
	return err == nil
}

func raw(f *os.File) (io.Reader, func(), error) {
	fd := f.Fd()
	old, err := getTermios(fd)
	if err != nil {
		return nil, nil, ErrNotTerminal
	}
	t := *old
	// libuv's UV_TTY_MODE_RAW.
	t.Iflag &^= syscall.BRKINT | syscall.ICRNL | syscall.INPCK | syscall.ISTRIP | syscall.IXON
	t.Oflag |= syscall.ONLCR
	t.Cflag |= syscall.CS8
	t.Lflag &^= syscall.ECHO | syscall.ICANON | syscall.IEXTEN | syscall.ISIG
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0
	if err := setTermios(fd, &t); err != nil {
		return nil, nil, err
	}
	return f, func() { _ = setTermios(fd, old) }, nil
}
