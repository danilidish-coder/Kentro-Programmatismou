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

func begin(task func(func(string))) {
	if ui.busy {
		return
	}
	ui.busy = true
	ui.jobCtx, ui.cancel = context.WithCancel(context.Background())
	ui.started = time.Now()
	ui.current = "Προετοιμασία"
	ui.completedPass, ui.completedFail, ui.completedSkipped = 0, 0, 0
	clearOutput()
	appendOutput("Εκτελούνται έλεγχοι. Τα πλήρη τεχνικά αποτελέσματα αποθηκεύονται αυτόματα.")
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
		begin(func(log func(string)) {
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
		if msgbox(ui.main, "Θα εκτελεστεί ο κώδικας του επιλεγμένου έργου στον υπολογιστή σας. Συνεχίζουμε ΜΟΝΟ αν το ZIP προέρχεται από έμπιστη πηγή;", "Επιβεβαίωση εκτέλεσης", mbYesNo|mbIconWarning) != idYes {
			return
		}
		begin(func(log func(string)) {
			r, e := ui.store.RunAll(ui.jobCtx, pID, log)
			if e != nil {
				log("ΣΦΑΛΜΑ: " + e.Error())
				return
			}
			log(fmt.Sprintf("Σύνολο: %d επιτυχίες, %d προβλήματα, %d μη εκτελεσμένα", countStatus(r, "PASS"), countNonPass(r, "FAIL"), countStatus(r, "NOT RUN")))
			if r.Blocked {
				missing := false
				for _, step := range r.Results {
					if step.Status == "NOT RUN" && strings.Contains(step.Reason, "Δεν βρέθηκε το απαραίτητο εργαλείο") {
						missing = true
					}
				}
				if missing {
					log("Χρειάζεται προετοιμασία υπολογιστή: λείπει εργαλείο που απαιτεί το έργο. Δεν σημαίνει ότι ο κώδικας απέτυχε.")
				}
				log("Οι υποχρεωτικοί έλεγχοι δεν ολοκληρώθηκαν. Η έκδοση παραμένει δοκιμαστική.")
			} else {
				// A non-programmer should not need a second technical approval button.
                // Promote only after the engine's mandatory gate has passed.
                stable, err := ui.store.Approve(pID)
                if err != nil {
                    log("Οι δοκιμές πέτυχαν αλλά η αποθήκευση EXE δεν ολοκληρώθηκε: " + err.Error())
                    return
                }
                log("Έτοιμη έκδοση: " + stable)
                exeFolder := filepath.Join(stable, "artifacts")
                if _, err := os.Stat(exeFolder); err == nil {
                    log("Ανοίγει ο φάκελος με το ελεγμένο EXE.")
                    _ = exec.Command("explorer.exe", exeFolder).Start()
                }
			}
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
		if r.Blocked {
			appendOutput("Δεν εγκρίθηκε νέα έκδοση: εκκρεμούν υποχρεωτικοί έλεγχοι.")
		} else {
			appendOutput("Όλοι οι υποχρεωτικοί έλεγχοι πέτυχαν. Το EXE αποθηκεύτηκε τοπικά.")
		}
	case 105:
		if msgbox(ui.main, "Η αναφορά περιλαμβάνει όνομα έργου, αποτελέσματα και βασικά στοιχεία συστήματος. Δεν περιλαμβάνει πηγαίο κώδικα ή ακατέργαστα logs. Ελέγξτε τη πριν τη στείλετε.", "Εξαγωγή αποτελεσμάτων", mbYesNo|0x40) != idYes {
			return
		}
		begin(func(log func(string)) {
			p, e := ui.store.ExportLatest(pID)
			if e != nil {
				log("Αποτυχία εξαγωγής: " + e.Error())
			} else {
				log("Δημιουργήθηκε πακέτο αναφοράς: " + p)
				exec.Command("explorer.exe", filepath.Dir(p)).Start()
			}
		})
	case 106:
		begin(func(log func(string)) {
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
		begin(func(log func(string)) {
			p, e := ui.store.RestoreNewest(pID)
			if e != nil {
				log("Αποτυχία: " + e.Error())
			} else {
				log("Επαναφορά σε νέο έργο: " + p.Name)
			}
		})
	case 108:
		begin(func(log func(string)) {
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
