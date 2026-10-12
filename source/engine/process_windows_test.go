//go:build windows

package engine

import (
	"os/exec"
	"testing"
)

func TestToolConsoleHidden(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit", "0")
	hideToolConsole(cmd)
	if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow || (cmd.SysProcAttr.CreationFlags&createNoWindow) == 0 {
		t.Fatal("External test/compiler command would open a black Windows console")
	}
}
