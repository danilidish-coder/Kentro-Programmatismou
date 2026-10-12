//go:build windows

package engine

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// Executing compiler/test tools must never flash a Windows console over our UI.
func hideToolConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
