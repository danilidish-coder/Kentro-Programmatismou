//go:build windows

package engine

import (
    "os/exec"
    "syscall"
)

// Suppress transient black console windows from test/build subprocesses.
func prepareChild(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
