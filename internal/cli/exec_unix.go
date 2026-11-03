//go:build unix

package cli

import (
	"os/exec"
	"syscall"
)

func hideWindow(*exec.Cmd) {}

// executable is fs.accessSync(path, X_OK).
func executable(path string) bool { return syscall.Access(path, 0x1) == nil }
