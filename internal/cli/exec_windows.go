//go:build windows

package cli

import (
	"os"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// hideWindow is Node.js's windowsHide: no console window flashes up for PowerShell.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// executable is fs.accessSync(path, X_OK), which on Windows only checks that the file is there.
func executable(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
