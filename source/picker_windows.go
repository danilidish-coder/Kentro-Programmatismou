//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// chooseZIPWindows uses a separate, STA file picker process instead of calling
// the legacy OPENFILENAME API inside the Go UI callback. PowerShell is included
// with supported Windows 10/11 installations; failure is non-fatal.
func chooseZIPWindows() (string, error) {
	tmp, err := os.CreateTemp("", "kentro-select-*.txt")
	if err != nil {
		return "", err
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := `$ErrorActionPreference = 'Stop';
Add-Type -AssemblyName System.Windows.Forms;
$f = New-Object System.Windows.Forms.OpenFileDialog;
$f.Title = 'Επιλογή έργου ZIP';
$f.Filter = 'Αρχεία ZIP (*.zip)|*.zip';
$f.CheckFileExists = $true;
$f.Multiselect = $false;
if ($f.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
 [System.IO.File]::WriteAllText(` + quote(path) + `, $f.FileName, (New-Object System.Text.UTF8Encoding($false)))
}
$f.Dispose();`
	cmd := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-STA", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ο διάλογος Windows απέτυχε: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(strings.TrimPrefix(string(data), "\ufeff"))
	if name == "" {
		return "", nil
	} // User cancelled.
	if !strings.EqualFold(filepath.Ext(name), ".zip") {
		return "", errors.New("επιλέχθηκε αρχείο που δεν είναι ZIP")
	}
	info, err := os.Stat(name)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("το επιλεγμένο ZIP δεν είναι κανονικό αρχείο")
	}
	return name, nil
}
