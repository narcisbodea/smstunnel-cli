//go:build !unix && !windows

package cli

import "syscall"

func errnoName(syscall.Errno) string { return "" }
