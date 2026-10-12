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
    // Preserve the active project when refreshing after tests or import.
    selectedID:=""
    index:=invoke(sendMessage,ui.list,lbGetCurSel,0,0)
    if index!=^uintptr(0) && int(index)<len(ui.ids) { selectedID=ui.ids[int(index)] }
    ui.ids = nil
	invoke(sendMessage, ui.list, lbResetContent, 0, 0)
	for _, p := range ps {
		ui.ids = append(ui.ids, p.ID)
		txt := fmt.Sprintf("%s  |  %s  |  %s", p.Name, p.Version, p.Language)
		invoke(sendMessage, ui.list, lbAddString, 0, uintptr(unsafe.Pointer(u16(txt))))
	}
    if ui.projectTitle!=0 {
        countLabel:=fmt.Sprintf("ΤΑ ΕΡΓΑ ΜΟΥ  ·  %d",len(ui.ids))
        invoke(sendMessage,ui.projectTitle,wmSetText,0,uintptr(unsafe.Pointer(u16(countLabel))))
    }
    if len(ui.ids)>0 {
        next:=0
        for i,id:=range ui.ids {if id==selectedID {next=i;break}}
        invoke(sendMessage,ui.list,lbSetCurSel,uintptr(next),0)
    }
    if ui.emptyHint!=0 {
        show:=uintptr(0)
        if len(ui.ids)==0 {show=5}
        invoke(showWindow,ui.emptyHint,show)
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
    if ui.list==0 || ui.store==nil || ui.busy {return}
    // Keep later-stage actions locked until their actual prerequisite is met.
    if btn:=ui.buttons[115];btn!=0 {invoke(enableWindow,btn,0)}
    if btn:=ui.buttons[116];btn!=0 {invoke(enableWindow,btn,0)}
    index:=invoke(sendMessage,ui.list,lbGetCurSel,0,0)
    if index==^uintptr(0)||int(index)>=len(ui.ids) {
        if ui.projectDetail!=0 {
            invoke(sendMessage,ui.projectDetail,wmSetText,0,uintptr(unsafe.Pointer(u16("ΕΠΙΛΕΓΜΕΝΟ ΠΡΟΓΡΑΜΜΑ\nΔεν έχει εισαχθεί ή επιλεγεί έργο"))))
        }
        showStage("1/4 · Εισάγετε ή επιλέξτε έργο")
        return
    }
    id:=ui.ids[int(index)]
    if ui.projectDetail!=0 {
        p,manifest,detailErr:=ui.store.Get(id)
        if detailErr==nil {
            ver:=p.Version
            if ver==""{ver="χωρίς αριθμό έκδοσης"}
            name:=p.Name
            if len([]rune(name))>34{name=string([]rune(name)[:31])+"..."}
            info:=fmt.Sprintf("ΕΠΙΛΕΓΜΕΝΟ ΠΡΟΓΡΑΜΜΑ: %s\nΈκδοση %s · %s · %d έλεγχοι / %d stress",name,ver,p.Language,len(manifest.Tests),len(manifest.Stress))
            invoke(sendMessage,ui.projectDetail,wmSetText,0,uintptr(unsafe.Pointer(u16(info))))
        } else {
            invoke(sendMessage,ui.projectDetail,wmSetText,0,uintptr(unsafe.Pointer(u16("Αδυναμία ανάγνωσης του επιλεγμένου έργου"))))
        }
    }
    run,err:=ui.store.LatestChecks(id)
    if err!=nil {showStage("2/4 · Αναμονή λειτουργικών ελέγχων και stress tests");return}
    if engine.IsChecksBlocked(run) {showStage("2/4 · Εντοπίστηκαν προβλήματα — EXE αποκλεισμένο");return}
    if err:=ui.store.ReadyForBuild(id);err!=nil {showStage("2/4 · Διαβάστε την αναφορά — "+err.Error());return}
    if btn:=ui.buttons[115];btn!=0 {invoke(enableWindow,btn,1)}
    if err:=ui.store.ReadyForSetup(id);err==nil {
        if btn:=ui.buttons[116];btn!=0 {invoke(enableWindow,btn,1)}
        showStage("4/4 · Το EXE πέρασε — προαιρετικό Setup διαθέσιμο");return
    }
    latest,err:=ui.store.LatestRun(id)
    if err==nil && latest.Stage==engine.StageBuild && !latest.Blocked {
        showStage("3/4 · Το EXE ολοκληρώθηκε — Setup μόνο με διαθέσιμη συνταγή")
        return
    }
    showStage("3/4 · Οι έλεγχοι πέρασαν — μπορείτε να κατασκευάσετε EXE")
}
