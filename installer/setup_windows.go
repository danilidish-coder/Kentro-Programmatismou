//go:build windows

package main

import (
	"embed"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

//go:embed assets/Kentro_Programmatismou.exe assets/Kentro_Programmatismou.ico
var assets embed.FS

func popup(text, title string, flags uintptr) uintptr {
	p := syscall.NewLazyDLL("user32.dll").NewProc("MessageBoxW")
	r, _, _ := p.Call(0, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(text))), uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(title))), flags)
	return r
}
func ps(script string) error {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(b)).Run()
}
func quotedPS(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
func shortcut(target, dest, icon string) error {
	script := fmt.Sprintf("$w=New-Object -ComObject WScript.Shell; $d=[Environment]::GetFolderPath('Desktop'); $s=$w.CreateShortcut((Join-Path $d 'Κέντρο Προγραμματισμού.lnk')); $s.TargetPath=%s; $s.WorkingDirectory=%s; $s.IconLocation=%s; $s.Save()", quotedPS(target), quotedPS(filepath.Dir(target)), quotedPS(icon))
	_ = dest
	return ps(script)
}
func main() {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		popup("Δεν βρέθηκε ο προσωπικός φάκελος εγκατάστασης.", "Σφάλμα", 0x10)
		return
	}
	install := filepath.Join(base, "Programs", "KentroProgrammatismou_Clean")
	exe := filepath.Join(install, "Kentro_Programmatismou.exe")
	home, e := os.UserHomeDir()
	if e != nil {
		popup(e.Error(), "Σφάλμα", 0x10)
		return
	}
	desktop := filepath.Join(home, "Desktop", "Κέντρο Προγραμματισμού.lnk")
	if (len(os.Args) > 1 && os.Args[1] == "/uninstall") || strings.EqualFold(filepath.Base(os.Args[0]), "Uninstall.exe") {
		if popup("Απεγκατάσταση της εφαρμογής; Τα προσωπικά έργα και οι αναφορές θα ΔΙΑΤΗΡΗΘΟΥΝ.", "Κέντρο Προγραμματισμού", 0x24) != 6 {
			return
		}
		os.Remove(exe)
		os.Remove(filepath.Join(install, "Kentro_Programmatismou.ico"))
		os.Remove(desktop)
		_ = ps("$d=[Environment]::GetFolderPath('Desktop'); Remove-Item -LiteralPath (Join-Path $d 'Κέντρο Προγραμματισμού.lnk') -Force -ErrorAction SilentlyContinue")
		// The running uninstaller cannot remove itself; a detached OS process finishes cleanup after exit.
		current, _ := os.Executable()
		dir := filepath.Dir(current)
		if strings.EqualFold(dir, install) {
			script := fmt.Sprintf("Start-Sleep -Seconds 2; Remove-Item -LiteralPath %s -Force -ErrorAction SilentlyContinue; Remove-Item -LiteralPath %s -Force -ErrorAction SilentlyContinue", quotedPS(current), quotedPS(dir))
			_ = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script).Start()
		}
		popup("Η απεγκατάσταση ολοκληρώθηκε. Τα έργα και τα αντίγραφα ασφαλείας διατηρήθηκαν.", "Κέντρο Προγραμματισμού", 0x40)
		return
	}
	if popup("Εγκατάσταση του νέου Κέντρου Προγραμματισμού στον προσωπικό λογαριασμό Windows; Δεν πειράζονται παλαιότερα προγράμματα.", "Εγκατάσταση", 0x24) != 6 {
		return
	}
	if e = os.MkdirAll(install, 0700); e != nil {
		popup(e.Error(), "Σφάλμα εγκατάστασης", 0x10)
		return
	}
	data, e := assets.ReadFile("assets/Kentro_Programmatismou.exe")
	if e != nil {
		popup(e.Error(), "Σφάλμα", 0x10)
		return
	}
	if e = os.WriteFile(exe, data, 0700); e != nil {
		popup(e.Error(), "Σφάλμα εγκατάστασης", 0x10)
		return
	}
	current, e := os.Executable()
	if e == nil {
		if b, e := os.ReadFile(current); e == nil {
			_ = os.WriteFile(filepath.Join(install, "Uninstall.exe"), b, 0700)
		}
	}
	iconData, _ := assets.ReadFile("assets/Kentro_Programmatismou.ico")
	icon := filepath.Join(install, "Kentro_Programmatismou.ico")
	if e = os.WriteFile(icon, iconData, 0600); e != nil {
		popup(e.Error(), "Σφάλμα εικονιδίου", 0x30)
	}
	if e = shortcut(exe, desktop, icon); e != nil {
		popup("Η εφαρμογή εγκαταστάθηκε, αλλά δεν δημιουργήθηκε η συντόμευση. Ανοίξτε: "+exe, "Εγκατάσταση", 0x30)
	} else {
		popup("Η εγκατάσταση ολοκληρώθηκε. Η συντόμευση βρίσκεται στην Επιφάνεια Εργασίας.", "Κέντρο Προγραμματισμού", 0x40)
	}
	_ = exec.Command(exe).Start()
}
