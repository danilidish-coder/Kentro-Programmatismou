//go:build !windows

package engine

import "os/exec"

func hideToolConsole(cmd *exec.Cmd) {}
