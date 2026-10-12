//go:build windows

package main

import (
	"kentro.local/programmatismou/engine"
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
	updateProjectStage()
}
func selected() string {
	n := invoke(sendMessage, ui.list, lbGetCurSel, 0, 0)
	if int(n) >= len(ui.ids) || n == ^uintptr(0) {
		msgbox(ui.main, "Επιλέξτε ένα έργο από τη λίστα.", "Επιλογή έργου", 0x40)
		return ""
	}
	return ui.ids[int(n)]
}

func showStage(value string) {
    if ui.stageStatus != 0 {
        invoke(sendMessage,ui.stageStatus,wmSetText,0,uintptr(unsafe.Pointer(u16(value))))
    }
}
func updateProjectStage() {
    if ui.list==0 || ui.store==nil {return}
    index:=invoke(sendMessage,ui.list,lbGetCurSel,0,0)
    if index==^uintptr(0)||int(index)>=len(ui.ids) {
        showStage("1/4 · Εισάγετε ή επιλέξτε έργο")
        return
    }
    id:=ui.ids[int(index)]
    run,err:=ui.store.LatestChecks(id)
    if err!=nil {showStage("2/4 · Αναμονή λειτουργικών ελέγχων και stress tests");return}
    if engine.IsChecksBlocked(run) {showStage("2/4 · Εντοπίστηκαν προβλήματα — EXE αποκλεισμένο");return}
    if err:=ui.store.ReadyForBuild(id);err!=nil {showStage("2/4 · Διαβάστε την αναφορά — "+err.Error());return}
    if err:=ui.store.ReadyForSetup(id);err==nil {showStage("4/4 · Το EXE πέρασε — προαιρετικό Setup διαθέσιμο");return}
    latest,err:=ui.store.LatestRun(id)
    if err==nil && latest.Stage==engine.StageBuild && !latest.Blocked {
        showStage("3/4 · Το EXE ολοκληρώθηκε — Setup μόνο με διαθέσιμη συνταγή")
        return
    }
    showStage("3/4 · Οι έλεγχοι πέρασαν — μπορείτε να κατασκευάσετε EXE")
}
