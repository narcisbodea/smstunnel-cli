//go:build !unix && !windows

package cli

import "os/exec"

func hideWindow(*exec.Cmd) {}

func executable(path string) bool { return fileExists(path) }
