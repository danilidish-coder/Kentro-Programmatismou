//go:build windows

package engine

import (
    "os/exec"
    "testing"
)

func TestAllWindowsChildCommandsHaveNoConsole(t *testing.T) {
    for _, executable := range []string{"cmd.exe", "powershell.exe", "python.exe", "go.exe", "taskkill.exe"} {
        cmd := exec.Command(executable, "/?")
        prepareChild(cmd)
        if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
            t.Fatalf("console window not suppressed for %s", executable)
        }
    }
}
