//go:build darwin || freebsd || openbsd || netbsd || dragonfly

package term

import "syscall"

const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)
