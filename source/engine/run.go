package engine

import (
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (s *Store) RunAll(ctx context.Context, id string, progress func(string)) (Run, error) {
	p, m, e := s.Get(id)
	if e != nil {
		return Run{}, e
	}
	base, _ := s.Base(id)
	source := filepath.Join(base, "source")
	hash, e := HashTree(source)
	if e != nil {
		return Run{}, e
	}
	plan, _ := json.Marshal(m)
	ph := sha256.Sum256(plan)
	run := Run{ID: newID(), ProjectID: id, SourceHash: hash, PlanHash: hex.EncodeToString(ph[:]), HostOS: runtime.GOOS, Started: time.Now()}
	dir := filepath.Join(base, "runs", run.ID)
	work := filepath.Join(dir, "work")
	if e = copyTree(source, work); e != nil {
		return run, e
	}
	execWork := work
	if m.WorkDir != "" && m.WorkDir != "." {
		execWork = filepath.Join(work, filepath.FromSlash(m.WorkDir))
		info, er := os.Stat(execWork)
		if er != nil || !info.IsDir() {
			return run, errors.New("Δεν βρέθηκε ο φάκελος πηγών που δηλώνει το σχέδιο ελέγχων")
		}
	}
	logdir := filepath.Join(dir, "logs")
	os.MkdirAll(logdir, 0700)
	add := func(r Result) {
		run.Results = append(run.Results, r)
		if progress != nil {
			progress(fmt.Sprintf("%s: %s — %s", r.Phase, r.Name, r.Status))
		}
	}
	if len(m.Tests) == 0 {
		add(Result{Name: "Δεν ορίστηκαν πραγματικά λειτουργικά τεστ", Phase: "TEST", Status: "NOT RUN", Required: true, Reason: "Χρειάζεται σχέδιο δοκιμών στο πακέτο έργου"})
	}
	for i, t := range m.Tests {
		if ctx.Err() != nil {
			add(Result{Name: t.Name, Phase: "TEST", Required: t.Mandatory(), Status: "CANCELLED", Reason: "Ακυρώθηκε"})
			continue
		}
		add(runStep(ctx, execWork, logdir, "TEST", fmt.Sprintf("test-%d", i), t))
	}
	if len(m.Stress) == 0 {
		add(Result{Name: "Δεν ορίστηκαν stress tests", Phase: "STRESS", Status: "NOT RUN", Required: true, Reason: "Απαιτείται ελέγξιμο σενάριο πίεσης"})
	}
	for i, sp := range m.Stress {
		add(runStress(ctx, execWork, logdir, i, sp))
	}
	beforeExe, _ := exeInventory(work)
	built := false
	if m.Build == nil {
		add(Result{Name: "Κατασκευή EXE", Phase: "BUILD", Status: "NOT RUN", Required: true, Reason: "Δεν υπάρχει συνταγή κατασκευής"})
	} else if runtime.GOOS != "windows" {
		add(Result{Name: m.Build.Name, Phase: "BUILD", Status: "NOT RUN", Required: true, Reason: "Απαιτείται πραγματικός υπολογιστής Windows"})
	} else {
		var br Result
		if reason := missingBuildRequirement(ctx, execWork, *m.Build); reason != "" {
			br = Result{Name: m.Build.Name, Phase: "BUILD", Required: m.Build.Mandatory(), Status: "NOT RUN", Reason: reason}
		} else {
			br = runStep(ctx, execWork, logdir, "BUILD", "build", *m.Build)
		}
		if br.Status == "PASS" {
			afterExe, er := exeInventory(work)
			if er != nil {
				br.Status = "FAIL"
				br.Reason = er.Error()
			}
			for relative, sum := range afterExe {
				if beforeExe[relative] == sum {
					continue
				}
				full := filepath.Join(work, filepath.FromSlash(relative))
				pefile, er := pe.Open(full)
				if er != nil {
					br.Status = "FAIL"
					br.Reason = "Το παραγόμενο αρχείο δεν είναι έγκυρο Windows EXE: " + relative
					continue
				}
				pefile.Close()
				st, er := os.Stat(full)
				if er != nil {
					br.Status = "FAIL"
					br.Reason = er.Error()
					continue
				}
				dest := filepath.Join(dir, "artifacts", filepath.FromSlash(relative))
				if er = copyFile(full, dest); er != nil {
					br.Status = "FAIL"
					br.Reason = er.Error()
					continue
				}
				run.Artifacts = append(run.Artifacts, Artifact{Name: relative, SHA256: sum, Bytes: st.Size()})
			}
			if len(run.Artifacts) == 0 {
				br.Status = "FAIL"
				br.Reason = "Η εντολή ολοκληρώθηκε αλλά δεν παρήγαγε νέο έγκυρο Windows EXE"
			}
		}
		add(br)
		built = br.Status == "PASS"
	}
	if m.Smoke == nil {
		add(Result{Name: "Πραγματική εκκίνηση", Phase: "SMOKE", Status: "NOT RUN", Required: true, Reason: "Δεν υπάρχει έλεγχος εκκίνησης του παραγόμενου EXE"})
	} else if runtime.GOOS != "windows" || !built {
		add(Result{Name: m.Smoke.Name, Phase: "SMOKE", Status: "NOT RUN", Required: true, Reason: "Δεν έγινε επιτυχής κατασκευή σε Windows"})
	} else {
		var sr Result
		if m.Smoke.ExpectContains == "WINDOW_STAYS_OPEN" {
			sr = runGUISmoke(ctx, execWork, *m.Smoke)
		} else {
			sr = runStep(ctx, execWork, logdir, "SMOKE", "smoke", *m.Smoke)
		}
		add(sr)
	}
	smokePassed := false
	for _, result := range run.Results {
		if result.Phase == "SMOKE" && result.Status == "PASS" {
			smokePassed = true
		}
	}
	for i, vis := range m.Visual {
		if !smokePassed {
			add(Result{Name: vis.Name, Phase: "VISUAL", Status: "NOT RUN", Required: vis.Mandatory(), Reason: "Απαιτείται πρώτα επιτυχής κατασκευή και εκκίνηση του EXE"})
			continue
		}
		vr := runStep(ctx, execWork, logdir, "VISUAL", fmt.Sprintf("visual-%d", i), vis)
		if vr.Status == "PASS" && strings.Contains(strings.Join(vis.Args, " "), "visual_flicker_windows.py") {
			vr = checkVisualResults(execWork, vr)
		}
		add(vr)
	}

	current, e := HashTree(source)
	if e != nil {
		return run, e
	}
	if current != hash {
		add(Result{Name: "Ακεραιότητα πηγών", Phase: "INTEGRITY", Status: "FAIL", Required: true, Reason: "Άλλαξαν τα αρχικά αρχεία κατά τη διάρκεια των ελέγχων"})
	} else {
		add(Result{Name: "Ακεραιότητα πηγών", Phase: "INTEGRITY", Status: "PASS", Required: true})
	}
	run.Finished = time.Now()
	run.Blocked = IsBlocked(run)
	if e = atomicJSON(filepath.Join(dir, "results.json"), run); e != nil {
		return run, e
	}
	if e = atomicWrite(filepath.Join(dir, "SUMMARY_GR.txt"), []byte(Summary(run, p))); e != nil {
		return run, e
	}
	// Only keep generated EXE and logs; isolate test runtime from original sources.
	return run, nil
}
func IsBlocked(r Run) bool {
	must := map[string]bool{"TEST": false, "STRESS": false, "BUILD": false, "SMOKE": false, "INTEGRITY": false}
	for _, v := range r.Results {
		if !v.Required {
			continue
		}
		if v.Status != "PASS" {
			return true
		}
		must[v.Phase] = true
	}
	for _, v := range must {
		if !v {
			return true
		}
	}
	return r.HostOS != "windows"
}
func Summary(r Run, p Project) string {
	var b strings.Builder
	fmt.Fprintf(&b, "ΚΕΝΤΡΟ ΠΡΟΓΡΑΜΜΑΤΙΣΜΟΥ — ΑΠΟΤΕΛΕΣΜΑΤΑ\nΈργο: %s\nΈκδοση: %s\nΈλεγχος: %s\nΣύστημα: %s\nSHA256 πηγών: %s\n", p.Name, p.Version, r.ID, r.HostOS, r.SourceHash)
	for _, v := range r.Results {
		fmt.Fprintf(&b, "[%s] %s: %s", v.Status, v.Phase, v.Name)
		if v.Reason != "" {
			fmt.Fprintf(&b, " — %s", v.Reason)
		}
		b.WriteString("\n")
	}
	if r.Stage==StageChecks {
		if IsChecksBlocked(r) {b.WriteString("ΕΛΕΓΧΟΙ: ΑΠΟΤΥΧΙΑ/ΕΚΚΡΕΜΟΤΗΤΕΣ — ΔΕΝ ΕΠΙΤΡΕΠΕΤΑΙ EXE\n")} else {
			b.WriteString("ΕΛΕΓΧΟΙ: ΕΠΙΤΥΧΙΑ — ΕΠΟΜΕΝΟ ΒΗΜΑ Η ΞΕΧΩΡΙΣΤΗ ΚΑΤΑΣΚΕΥΗ EXE\n")
		}
	} else if r.Stage==StageSetup {
		if r.Blocked {b.WriteString("SETUP: ΑΠΟΤΥΧΙΑ\n")} else {b.WriteString("SETUP: ΔΗΜΙΟΥΡΓΗΘΗΚΕ\n")}
	} else if r.Blocked {
		b.WriteString("ΑΠΟΤΕΛΕΣΜΑ: ΑΠΟΚΛΕΙΣΤΗΚΕ Η ΣΤΑΘΕΡΗ ΕΚΔΟΣΗ\n")
	} else {b.WriteString("ΑΠΟΤΕΛΕΣΜΑ: ΟΛΟΙ ΟΙ ΥΠΟΧΡΕΩΤΙΚΟΙ ΕΛΕΓΧΟΙ ΠΕΡΑΣΑΝ\n")}
	return b.String()
}
