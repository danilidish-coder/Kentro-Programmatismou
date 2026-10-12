//go:build windows

package main

import (
	"context"
	"fmt"
	"kentro.local/programmatismou/engine"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func begin(stage string, task func(func(string))) {
	if ui.busy {
		return
	}
	ui.busy = true
	ui.jobCtx, ui.cancel = context.WithCancel(context.Background())
	ui.started = time.Now()
	ui.current = "Προετοιμασία"
	ui.stage = stage
	showStage("Εκτελείται: " + stage)
	ui.completedPass, ui.completedFail, ui.completedSkipped = 0, 0, 0
	clearOutput()
	appendOutput("Στάδιο: " + stage + ". Οι ενέργειες εκτελούνται μία-μία και τα αποτελέσματα αποθηκεύονται.")
	for id, bt := range ui.buttons {
		if id != 111 {
			invoke(enableWindow, bt, 0)
		} else {
			invoke(enableWindow, bt, 1)
		}
	}
	go func() { defer queue(event{done: true, refresh: true}); task(func(s string) { queue(event{line: s}) }) }()
}
func displayZIPName(path string) string {
	base := filepath.Base(path)
	if decoded, e := url.PathUnescape(base); e == nil && decoded != "" {
		return decoded
	}
	return base
}

func onButton(id int) {

	if id == 111 {
		if ui.cancel != nil {
			ui.cancel()
			appendOutput("Ζητήθηκε ακύρωση ελέγχου")
		}
		return
	}
	if id == 110 {
		refreshList()
		return
	}
	if ui.busy {
		return
	}
	if id == 101 {
		file := openZip()
		if file == "" {
			return
		}
		preview, er := engine.PreviewZIP(file)
		if er != nil {
			msgbox(ui.main, "Το ZIP απορρίφθηκε: "+er.Error(), "Έλεγχος εισαγωγής", 0x10)
			return
		}
		message := fmt.Sprintf("Αρχείο: %s\nΠεριεχόμενο: %d αρχεία (%.1f MB ασυμπίεστα)\nΈτοιμες οδηγίες δοκιμής: %t\n\nΘέλετε να εισαχθεί ως νέο έργο;", displayZIPName(file), preview.Files, float64(preview.SizeBytes)/1048576, preview.ContainsManifest)
		if msgbox(ui.main, message, "Προεπισκόπηση ZIP", mbYesNo|0x40) != idYes {
			return
		}
		begin("Εισαγωγή έργου", func(log func(string)) {
			log("Παραλαβή έργου: " + displayZIPName(file))
			p, e := ui.store.ImportZIP(file)
			if e != nil {
				log("ΑΠΟΤΥΧΙΑ: " + e.Error())
			} else {
				log("Επιτυχής εισαγωγή έργου: " + p.Name + " — " + p.Language)
			}
		})
		return
	}
	if id == 114 {
		if msgbox(ui.main, "Να επανέλθει στη λίστα το πιο πρόσφατα αφαιρεμένο έργο;", "Επαναφορά έργου", mbYesNo|0x40) != idYes {
			return
		}
		p, e := ui.store.RestoreLatestTrashed()
		if e != nil {
			appendOutput("Δεν έγινε επαναφορά: " + e.Error())
			return
		}
		appendOutput("Το έργο επανήλθε: " + p.Name)
		refreshList()
		return
	}
	pID := selected()
	if pID == "" {
		return
	}
	switch id {
	case 102:
		p, m, e := ui.store.Get(pID)
		if e != nil {
			appendOutput(e.Error())
			return
		}
		buildText := "Δεν βρέθηκαν ακόμη οδηγίες κατασκευής"
		if m.Build != nil {
			buildText = "Η κατασκευή EXE έχει αναγνωριστεί"
		}
		startText := "Δεν υπάρχει ακόμη σχέδιο εκκίνησης"
		if m.Smoke != nil {
			startText = "Θα ελεγχθεί η πραγματική εκκίνηση"
		}
		appendOutput(fmt.Sprintf("Έργο: %s\nΓλώσσα: %s\nΈκδοση: %s\nΛειτουργικοί έλεγχοι: %d\nΔοκιμές πίεσης: %d\nΟπτικοί έλεγχοι: %d\n%s\n%s\n\nΕΡΓΑΛΕΙΑ ΥΠΟΛΟΓΙΣΤΗ:", p.Name, p.Language, p.Version, len(m.Tests), len(m.Stress), len(m.Visual), buildText, startText))
		for _, line := range engine.Readiness(m) {
			appendOutput(line)
		}
	case 103:
        if msgbox(ui.main,"Θα εκτελεστούν μόνο λειτουργικοί έλεγχοι και stress tests στον υπολογιστή. ΔΕΝ θα κατασκευαστεί EXE. Εκτελούμε κώδικα μόνο από έμπιστη πηγή. Συνέχεια;","Στάδιο 2: Έλεγχοι",mbYesNo|mbIconWarning)!=idYes{return}
        begin("2/4 · Έλεγχοι και stress tests",func(log func(string)){
            log("Ξεκινούν λειτουργικοί έλεγχοι και δοκιμές πίεσης. Δεν κατασκευάζεται EXE.")
            r,e:=ui.store.RunChecks(ui.jobCtx,pID,log)
            if e!=nil {log("ΣΦΑΛΜΑ: "+e.Error());return}
            log(fmt.Sprintf("Ολοκληρώθηκαν: %d επιτυχίες, %d προβλήματα, %d παραλείψεις",countStatus(r,"PASS"),countNonPass(r,"FAIL"),countStatus(r,"NOT RUN")))
            if r.Blocked {
                log("Οι έλεγχοι ΔΕΝ πέρασαν. Η δημιουργία EXE αποκλείστηκε.")
            } else {
                log("Οι έλεγχοι πέρασαν. Μπορείτε να μελετήσετε την αναφορά και μετά να ζητήσετε EXE.")
            }
            filename,err:=ui.store.ExportLatest(pID)
            if err!=nil {log("Σφάλμα αυτόματης αναφοράς: "+err.Error())} else {log("Η αναφορά αποθηκεύτηκε: "+filename)}
        })
    case 115:
        if err:=ui.store.ReadyForBuild(pID);err!=nil{
            msgbox(ui.main,"Δεν επιτρέπεται ακόμη η δημιουργία EXE. "+err.Error(),"Στάδιο 3: Προϋποθέσεις",0x30)
            return
        }
        if msgbox(ui.main,"Οι λειτουργικοί έλεγχοι και τα stress tests πέρασαν. Να ξεκινήσει ΤΩΡΑ, ως ξεχωριστό στάδιο, η κατασκευή και πραγματική δοκιμή EXE;","Στάδιο 3: Δημιουργία EXE",mbYesNo|0x40)!=idYes{return}
        begin("3/4 · Δημιουργία και δοκιμή EXE",func(log func(string)){
            r,e:=ui.store.BuildChecked(ui.jobCtx,pID,log)
            if e!=nil {log("ΣΦΑΛΜΑ: "+e.Error());return}
            if r.Blocked{log("Το EXE δεν πέρασε τον τελικό έλεγχο. Δεν εγκρίθηκε καμία έκδοση.");return}
            stable,err:=ui.store.Approve(pID)
            if err!=nil {log("Αποτυχία αποθήκευσης εγκεκριμένου EXE: "+err.Error());return}
            log("Έτοιμο και ελεγμένο EXE: "+stable)
            exeFolder:=filepath.Join(stable,"artifacts")
            if _,err:=os.Stat(exeFolder);err==nil{_ = exec.Command("explorer.exe",exeFolder).Start()}
        })
    case 116:
        if err:=ui.store.ReadyForSetup(pID);err!=nil{
            msgbox(ui.main,err.Error(),"Στάδιο 4: Προαιρετικό Setup",0x40)
            return
        }
        if msgbox(ui.main,"Η κατασκευή Setup είναι προαιρετική και ξεχωριστή. Να ξεκινήσει;","Στάδιο 4: Δημιουργία Setup",mbYesNo|0x40)!=idYes{return}
        begin("4/4 · Προαιρετικό Setup",func(log func(string)){
            r,e:=ui.store.BuildSetup(ui.jobCtx,pID,log)
            if e!=nil {log("ΣΦΑΛΜΑ: "+e.Error());return}
            if r.Blocked{log("Η δημιουργία Setup απέτυχε. Το ήδη ελεγμένο EXE παραμένει ανέπαφο.");return}
            base,err:=ui.store.Base(pID)
            if err!=nil{return}
            folder:=filepath.Join(base,"runs",r.ID,"artifacts")
            log("Το Setup βρίσκεται στον φάκελο: "+folder)
            _ = exec.Command("explorer.exe",folder).Start()
        })
	case 104:
		r, e := ui.store.LatestRun(pID)
		if e != nil {
			appendOutput(e.Error())
			return
		}
		p, _, e := ui.store.Get(pID)
		if e != nil {
			appendOutput(e.Error())
			return
		}
		clearOutput()
		appendOutput("Έργο: " + p.Name)
		passed, failed, skipped := 0, 0, 0
		for _, step := range r.Results {
			switch step.Status {
			case "PASS":
				passed++
			case "NOT RUN":
				skipped++
			default:
				failed++
			}
		}
		appendOutput(fmt.Sprintf("Επιτυχίες: %d  |  Προβλήματα: %d  |  Δεν εκτελέστηκαν: %d", passed, failed, skipped))
		for _, step := range r.Results {
			if step.Status != "PASS" {
				appendOutput(step.Phase + ": " + step.Name + " — " + step.Status + " " + step.Reason)
			}
		}
		if r.Stage==engine.StageChecks {
            if r.Blocked{appendOutput("Στάδιο 2: Οι έλεγχοι απέτυχαν — EXE δεν επιτρέπεται.")} else {appendOutput("Στάδιο 2: Επιτυχής αναφορά. Επόμενο: ξεχωριστή δημιουργία EXE.")}
        } else if r.Stage==engine.StageBuild {
            if r.Blocked{appendOutput("Στάδιο 3: Η κατασκευή ή η εκκίνηση απέτυχε.")} else {appendOutput("Στάδιο 3: Το EXE κατασκευάστηκε και ελέγχθηκε.")}
        } else if r.Blocked {appendOutput("Η εργασία δεν εγκρίθηκε.")} else {appendOutput("Η εργασία ολοκληρώθηκε.")}
	case 105:
		if msgbox(ui.main, "Η αναφορά περιλαμβάνει όνομα έργου, αποτελέσματα και βασικά στοιχεία συστήματος. Δεν περιλαμβάνει πηγαίο κώδικα ή ακατέργαστα logs. Ελέγξτε τη πριν τη στείλετε.", "Εξαγωγή αποτελεσμάτων", mbYesNo|0x40) != idYes {
			return
		}
		begin("Αποθήκευση αναφοράς", func(log func(string)) {
			p, e := ui.store.ExportLatest(pID)
			if e != nil {
				log("Αποτυχία εξαγωγής: " + e.Error())
			} else {
				log("Δημιουργήθηκε πακέτο αναφοράς: " + p)
				exec.Command("explorer.exe", filepath.Dir(p)).Start()
			}
		})
	case 106:
		begin("Αντίγραφο ασφαλείας", func(log func(string)) {
			p, e := ui.store.Backup(pID)
			if e != nil {
				log("Αποτυχία αντιγράφου: " + e.Error())
			} else {
				log("Ασφαλές αντίγραφο: " + p)
			}
		})
	case 107:
		if msgbox(ui.main, "Θα δημιουργηθεί ΝΕΟ ανεξάρτητο έργο από το πιο πρόσφατο αντίγραφο. Δεν θα αντικατασταθεί κανένα παλιό αρχείο.", "Επαναφορά", mbYesNo|0x40) != idYes {
			return
		}
		begin("Επαναφορά έργου", func(log func(string)) {
			p, e := ui.store.RestoreNewest(pID)
			if e != nil {
				log("Αποτυχία: " + e.Error())
			} else {
				log("Επαναφορά σε νέο έργο: " + p.Name)
			}
		})
	case 108:
		begin("Έγκριση σταθερής έκδοσης", func(log func(string)) {
			p, e := ui.store.Approve(pID)
			if e != nil {
				log("ΑΠΟΚΛΕΙΣΜΟΣ: " + e.Error())
			} else {
				log("Εγκρίθηκε σταθερή έκδοση και αποθηκεύτηκε: " + p)
			}
		})
	case 113:
		p, _, e := ui.store.Get(pID)
		if e != nil {
			appendOutput("Αδυναμία εντοπισμού έργου: " + e.Error())
			return
		}
		question := fmt.Sprintf("Να αφαιρεθεί το έργο «%s» από τη λίστα;\n\nΔεν θα χαθεί: θα φυλαχθεί σε τοπικό φάκελο ανάκτησης. Μπορείτε να το επαναφέρετε με το κουμπί «Επαναφορά αφαιρεμένου».", p.Name)
		if msgbox(ui.main, question, "Επιβεβαίωση αφαίρεσης", mbYesNo|mbIconWarning) != idYes {
			return
		}
		name, e := ui.store.TrashProject(pID)
		if e != nil {
			appendOutput("Δεν αφαιρέθηκε το έργο: " + e.Error())
			return
		}
		appendOutput("Το έργο «" + name + "» αφαιρέθηκε από τη λίστα. Υπάρχει αντίγραφο ανάκτησης.")
		refreshList()
	case 109:
		p, e := ui.store.Base(pID)
		if e == nil {
			exec.Command("explorer.exe", p).Start()
		}
	}
}
func countStatus(r engine.Run, status string) int {
	count := 0
	for _, step := range r.Results {
		if step.Status == status {
			count++
		}
	}
	return count
}
func countNonPass(r engine.Run, status string) int {
	count := 0
	for _, step := range r.Results {
		if step.Status == status || step.Status == "CANCELLED" {
			count++
		}
	}
	return count
}
func openZip() string {
	// File chooser runs in a separate PowerShell process. A native dialog fault
	// cannot bring down the main application or corrupt its project store.
	file, err := chooseZIPWindows()
	if err != nil {
		appendOutput("Πρόβλημα στην επιλογή αρχείου: " + err.Error())
		msgbox(ui.main, "Δεν ήταν δυνατή η επιλογή ZIP. Η εφαρμογή παραμένει ανοιχτή.\n\n"+err.Error(), "Επιλογή ZIP", 0x10)
		return ""
	}
	return strings.TrimSpace(file)
}
