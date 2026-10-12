//go:build windows

package main

import (
	"fmt"
	"sort"
	"strings"
	"unsafe"
)

func queue(ev event) {
	ui.mu.Lock()
	ui.pending = append(ui.pending, ev)
	ui.mu.Unlock()
	invoke(postMessage, ui.main, wmApp+3, 0, 0)
}
func appendOutput(s string) {
	// Native list box draws each outcome in an independent row (no overprinting).
	if ui.output == 0 {
		return
	}
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ui.logs += line + "\n"
		invoke(sendMessage, ui.output, lbAddString, 0, uintptr(unsafe.Pointer(u16(line))))
	}
	count := invoke(sendMessage, ui.output, lbGetCount, 0, 0)
	for count > 250 {
		invoke(sendMessage, ui.output, lbDeleteString, 0, 0)
		count--
	}
	if count > 0 {
		visibleRows := uintptr(13)
		if count > visibleRows {
			invoke(sendMessage, ui.output, lbSetTopIndex, count-visibleRows, 0)
		}
	}
}
func clearOutput() {
	ui.logs = ""
	if ui.output != 0 {
		invoke(sendMessage, ui.output, lbResetContent, 0, 0)
	}
}
func refreshList() {
	if ui.list == 0 || ui.store == nil {
		return
	}
	ps, e := ui.store.List()
	if e != nil {
		appendOutput("Σφάλμα λίστας: " + e.Error())
		return
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ImportedAt.After(ps[j].ImportedAt) })
	ui.ids = nil
	invoke(sendMessage, ui.list, lbResetContent, 0, 0)
	for _, p := range ps {
		ui.ids = append(ui.ids, p.ID)
		txt := fmt.Sprintf("%s  |  %s  |  %s", p.Name, p.Version, p.Language)
		invoke(sendMessage, ui.list, lbAddString, 0, uintptr(unsafe.Pointer(u16(txt))))
	}
	if len(ui.ids) > 0 {
		invoke(sendMessage, ui.list, lbSetCurSel, 0, 0)
	}
}
func selected() string {
	n := invoke(sendMessage, ui.list, lbGetCurSel, 0, 0)
	if int(n) >= len(ui.ids) || n == ^uintptr(0) {
		msgbox(ui.main, "Επιλέξτε ένα έργο από τη λίστα.", "Επιλογή έργου", 0x40)
		return ""
	}
	return ui.ids[int(n)]
}
