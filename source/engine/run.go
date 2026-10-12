package engine

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	if r.Blocked {
		b.WriteString("ΑΠΟΤΕΛΕΣΜΑ: ΑΠΟΚΛΕΙΣΤΗΚΕ Η ΣΤΑΘΕΡΗ ΕΚΔΟΣΗ\n")
	} else {
		b.WriteString("ΑΠΟΤΕΛΕΣΜΑ: ΟΛΟΙ ΟΙ ΥΠΟΧΡΕΩΤΙΚΟΙ ΕΛΕΓΧΟΙ ΠΕΡΑΣΑΝ\n")
	}
	return b.String()
}
func exeInventory(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(info.Name()), ".exe") {
			return nil
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		out[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return out, err
}
func copyFile(src, dst string) error {
	if e := os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return e
	}
	a, e := os.Open(src)
	if e != nil {
		return e
	}
	defer a.Close()
	b, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(b, a)
	e2 := b.Close()
	if e != nil {
		return e
	}
	return e2
}

// Build availability is checked before running a time-consuming compiler.
// Missing packages are NOT RUN, not a source-code failure.
func missingBuildRequirement(ctx context.Context, work string, step Step) string {
	if strings.EqualFold(step.Command, "python") && len(step.Args) >= 2 && step.Args[0] == "-m" && step.Args[1] == "PyInstaller" {
		python, err := exec.LookPath("python")
		if err != nil {
			return "Δεν είναι διαθέσιμη η Python στον υπολογιστή"
		}
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(check, python, "-c", "import importlib.util,sys;sys.exit(0 if importlib.util.find_spec('PyInstaller') else 5)")
		prepareChild(cmd)
		cmd.Dir = work
		cmd.Env = offlineEnv()
		if err = cmd.Run(); err != nil {
			return "Λείπει το εργαλείο κατασκευής PyInstaller. Χρειάζεται τοπική προετοιμασία χωρίς αυτόματη σύνδεση στο internet"
		}
	}
	return ""
}

// GUI smoke test: a desktop program must stay open briefly. A controlled kill
// after the interval is PASS; unexpected early termination is FAIL.
// PyInstaller one-file EXEs spawn children: kill the entire Windows process tree.
func terminateProcessTree(cmd *exec.Cmd) {
    if cmd == nil || cmd.Process == nil {
        return
    }
    if runtime.GOOS == "windows" {
        ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
        defer cancel()
        taskkill := exec.CommandContext(ctx, "taskkill.exe", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
        prepareChild(taskkill)
        _ = taskkill.Run()
    }
    _ = cmd.Process.Kill()
}

func runGUISmoke(ctx context.Context, work string, step Step) Result {
	r := Result{Name: step.Name, Phase: "SMOKE", Required: step.Mandatory(), ExitCode: -1}
	exe, args, err := resolve(work, step.Command, step.Args)
	if err != nil {
		r.Status = "NOT RUN"
		r.Reason = err.Error()
		return r
	}
	cmd := exec.Command(exe, args...)
	prepareChild(cmd)
	cmd.Dir = work
	cmd.Env = offlineEnv()
	start := time.Now()
	if err = cmd.Start(); err != nil {
		r.Status = "FAIL"
		r.Reason = redact(err.Error())
		return r
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	select {
	case err := <-done:
		r.Status = "FAIL"
		r.Reason = "Το γραφικό πρόγραμμα τερματίστηκε πριν ολοκληρωθεί ο έλεγχος εκκίνησης"
		if err != nil {
			r.Reason += ": " + redact(err.Error())
		}
	case <-timer.C:
		terminateProcessTree(cmd)
		<-done
		r.Status = "PASS"
		r.ExitCode = 0
		r.Reason = "Η εφαρμογή παρέμεινε σε λειτουργία για τουλάχιστον 4 δευτερόλεπτα"
	case <-ctx.Done():
		terminateProcessTree(cmd)
		<-done
		r.Status = "CANCELLED"
		r.Reason = "Ακυρώθηκε ο έλεγχος εκκίνησης"
	}
	if runtime.GOOS == "windows" {
        time.Sleep(350 * time.Millisecond)
    }
    r.DurationMS = time.Since(start).Milliseconds()
	return r
}

// The upstream flicker tool exits with code zero even when it detects flicker.
// Read its structured evidence rather than equating exit code with success.
func checkVisualResults(work string, r Result) Result {
	path := filepath.Join(work, "visual_checks", "report.json")
	data, err := os.ReadFile(path)
	if err != nil {
		r.Status = "FAIL"
		r.Reason = "Δεν δημιουργήθηκε η αναφορά οπτικού ελέγχου"
		return r
	}
	var report struct {
		Startup int               `json:"startup_endpoint_flicker_runs"`
		Theme   int               `json:"theme_endpoint_flicker_toggles"`
		Clicks  int               `json:"theme_clicks_confirmed"`
		Runs    []json.RawMessage `json:"startup_runs"`
		Toggles []json.RawMessage `json:"theme_toggles"`
	}
	if json.Unmarshal(data, &report) != nil || len(report.Runs) == 0 || len(report.Toggles) == 0 {
		r.Status = "FAIL"
		r.Reason = "Ελλιπής αναφορά οπτικού ελέγχου"
		return r
	}
	if report.Startup > 0 || report.Theme > 0 || report.Clicks == 0 {
		r.Status = "FAIL"
		r.Reason = fmt.Sprintf("Οπτικά ευρήματα: %d εκκινήσεις, %d αλλαγές θέματος, %d επιβεβαιωμένα κλικ", report.Startup, report.Theme, report.Clicks)
		return r
	}
	r.Reason = fmt.Sprintf("Έλεγχος εικόνας: %d εκκινήσεις, %d αλλαγές θέματος χωρίς καταγεγραμμένη απόκλιση", len(report.Runs), len(report.Toggles))
	return r
}

func runStep(ctx context.Context, work, logDir, phase, basename string, t Step) Result {
	r := Result{Name: t.Name, Phase: phase, Required: t.Mandatory(), ExitCode: -1}
	if r.Name == "" {
		r.Name = t.Command
	}
	if e := validateStep(t); e != nil {
		r.Status = "FAIL"
		r.Reason = e.Error()
		return r
	}
	exe, args, err := resolve(work, t.Command, t.Args)
	if err != nil {
		r.Status = "NOT RUN"
		r.Reason = err.Error()
		return r
	}
	sec := t.TimeoutSeconds
	if sec == 0 {
		sec = 90
	}
	ctx2, cancel := context.WithTimeout(ctx, time.Duration(sec)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx2, exe, args...)
	prepareChild(cmd)
	cmd.Dir = work
	cmd.Env = offlineEnv()
	var b bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &b, n: 160000}
	cmd.Stderr = &limitedWriter{w: &b, n: 160000}
	start := time.Now()
	e := cmd.Run()
	r.DurationMS = time.Since(start).Milliseconds()
	r.Command = redact(strings.Join(append([]string{t.Command}, t.Args...), " "))
	if e == nil {
		r.Status = "PASS"
		r.ExitCode = 0
		if t.ExpectContains != "" && !strings.Contains(b.String(), t.ExpectContains) {
			r.Status = "FAIL"
			r.Reason = "Η έξοδος δεν περιέχει το αναμενόμενο αποτέλεσμα"
		}
	} else {
		r.Status = "FAIL"
		if ctx2.Err() != nil {
			r.Reason = "Υπέρβαση χρονικού ορίου ή ακύρωση"
		} else {
			r.Reason = redact(e.Error())
		}
		if x, ok := e.(*exec.ExitError); ok {
			r.ExitCode = x.ExitCode()
		}
	}
	if b.Len() != 0 {
		f := filepath.Join(logDir, basename+".txt")
		if er := atomicWrite(f, []byte(redact(b.String()))); er == nil {
			r.LogFile = filepath.Base(f)
		}
	}
	return r
}
func resolve(work, command string, args []string) (string, []string, error) {
	command = strings.TrimSpace(command)
	allowed := map[string]bool{"go": true, "python": true, "python3": true, "py": true, "pytest": true, "pyinstaller": true, "dotnet": true, "node": true, "npm": true, "cargo": true}
	if allowed[strings.ToLower(command)] {
		x, e := exec.LookPath(command)
		if e != nil {
			return "", nil, fmt.Errorf("Δεν βρέθηκε το απαραίτητο εργαλείο: %s", command)
		}
		return x, args, nil
	}
	// Relative executables are permitted only inside the isolated working copy.
	c := strings.ReplaceAll(command, "\\", "/")
	if !strings.HasPrefix(c, "./") || !strings.HasSuffix(strings.ToLower(c), ".exe") || !zipPathOK(strings.TrimPrefix(c, "./")) {
		return "", nil, errors.New("Η συνταγή ζητά μη επιτρεπτή εντολή")
	}
	rel := strings.TrimPrefix(c, "./")
	full := filepath.Join(work, filepath.FromSlash(rel))
	if !exists(full) {
		return "", nil, errors.New("Δεν δημιουργήθηκε το εκτελέσιμο: " + rel)
	}
	return full, args, nil
}
func offlineEnv() []string {
	out := []string{}
	for _, v := range os.Environ() {
		up := strings.ToUpper(strings.SplitN(v, "=", 2)[0])
		if up == "GOPROXY" || up == "GOSUMDB" || up == "GOTOOLCHAIN" || up == "PIP_NO_INDEX" || up == "PIP_DISABLE_PIP_VERSION_CHECK" {
			continue
		}
		out = append(out, v)
	}
	return append(out, "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "PIP_NO_INDEX=1", "PIP_DISABLE_PIP_VERSION_CHECK=1")
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	n := len(p)
	if l.n > 0 {
		k := n
		if k > l.n {
			k = l.n
		}
		l.w.Write(p[:k])
		l.n -= k
	}
	return n, nil
}
func runStress(ctx context.Context, work, logs string, i int, sp StressSpec) Result {
	r := Result{Name: sp.Name, Phase: "STRESS", Required: sp.Mandatory(), ExitCode: -1}
	if r.Name == "" {
		r.Name = "Σενάριο πίεσης"
	}
	if ctx.Err() != nil {
		r.Status = "CANCELLED"
		return r
	}
	if sp.Kind == "restart" {
		if sp.Health == nil {
			r.Status = "NOT RUN"
			r.Reason = "Απουσιάζει υποχρεωτικός έλεγχος υγείας μετά την επανεκκίνηση"
			return r
		}
		return restartStress(ctx, work, logs, i, sp)
	}
	// A missing compiler means NOT RUN, not a failed stress scenario.
	// Resolve once before starting parallel workers; never count setup errors as failures.
	if _, _, err := resolve(work, sp.Command, sp.Args); err != nil {
		r.Status = "NOT RUN"
		r.Reason = err.Error()
		return r
	}
	count := sp.Repeats
	if count <= 0 {
		count = 3
	}
	parallel := sp.Parallel
	if parallel <= 0 {
		parallel = 2
	}
	start := time.Now()
	jobs := make(chan int)
	errs := make(chan string, count)
	var wg sync.WaitGroup
	for w := 0; w < parallel; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if ctx.Err() != nil {
					errs <- "Ακυρώθηκε"
					continue
				}
				st := runStep(ctx, work, logs, "STRESS", fmt.Sprintf("stress-%d-%03d", i, j), Step{Name: sp.Name, Command: sp.Command, Args: sp.Args, TimeoutSeconds: sp.TimeoutSeconds})
				if st.Status != "PASS" {
					errs <- st.Status + ": " + st.Reason
				} else {
					errs <- ""
				}
			}
		}()
	}
	for j := 0; j < count; j++ {
		jobs <- j
	}
	close(jobs)
	wg.Wait()
	close(errs)
	bad := 0
	reason := ""
	for s := range errs {
		if s != "" {
			bad++
			if reason == "" {
				reason = s
			}
		}
	}
	r.DurationMS = time.Since(start).Milliseconds()
	if bad > 0 {
		r.Status = "FAIL"
		r.Reason = fmt.Sprintf("%d/%d αποτυχίες: %s", bad, count, reason)
	} else {
		r.Status = "PASS"
		r.ExitCode = 0
		r.Reason = fmt.Sprintf("%d επαναλήψεις, έως %d παράλληλες εκτελέσεις", count, parallel)
	}
	return r
}
func restartStress(ctx context.Context, work, logs string, i int, sp StressSpec) Result {
	r := Result{Name: sp.Name, Phase: "STRESS", Required: sp.Mandatory(), ExitCode: -1}
	reps := sp.Repeats
	if reps < 1 {
		reps = 2
	}
	start := time.Now()
	exe, args, e := resolve(work, sp.Command, sp.Args)
	if e != nil {
		r.Status = "NOT RUN"
		r.Reason = e.Error()
		return r
	}
	for k := 0; k < reps; k++ {
		if ctx.Err() != nil {
			r.Status = "CANCELLED"
			return r
		}
		cmd := exec.Command(exe, args...)
		prepareChild(cmd)
		cmd.Dir = work
		cmd.Env = offlineEnv()
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if e := cmd.Start(); e != nil {
			r.Status = "FAIL"
			r.Reason = e.Error()
			return r
		}
		time.Sleep(250 * time.Millisecond)
		cmd.Process.Kill()
		cmd.Wait()
		st := runStep(ctx, work, logs, "STRESS", fmt.Sprintf("restart-%d-%d", i, k), *sp.Health)
		if st.Status != "PASS" {
			r.Status = "FAIL"
			r.Reason = "Αποτυχία ανάκαμψης μετά τον τερματισμό: " + st.Status + " " + st.Reason
			return r
		}
	}
	r.Status = "PASS"
	r.ExitCode = 0
	r.DurationMS = time.Since(start).Milliseconds()
	r.Reason = fmt.Sprintf("%d αναγκαστικοί τερματισμοί και επιτυχή health checks", reps)
	return r
}
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		to := filepath.Join(dst, rel)
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Δεν επιτρέπονται συμβολικοί σύνδεσμοι")
		}
		if info.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("Μη κανονικό αρχείο")
		}
		if e = os.MkdirAll(filepath.Dir(to), 0700); e != nil {
			return e
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		e2 := out.Close()
		if e != nil {
			return e
		}
		return e2
	})
}
func redact(s string) string {
	for _, key := range []string{"TOKEN", "PASSWORD", "API_KEY", "SECRET"} {
		v := os.Getenv(key)
		if len(v) >= 6 {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	home, _ := os.UserHomeDir()
	if len(home) > 3 {
		s = strings.ReplaceAll(s, home, "[HOME]")
	}
	return s
}
func (s *Store) LatestRun(id string) (Run, error) {
	var r Run
	root, e := s.Base(id)
	if e != nil {
		return r, e
	}
	entries, e := os.ReadDir(filepath.Join(root, "runs"))
	if e != nil {
		return r, e
	}
	type rec struct{ run Run }
	all := []Run{}
	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		b, e := os.ReadFile(filepath.Join(root, "runs", d.Name(), "results.json"))
		if e != nil {
			continue
		}
		var x Run
		if json.Unmarshal(b, &x) == nil && x.ProjectID == id {
			all = append(all, x)
		}
	}
	if len(all) == 0 {
		return r, errors.New("Δεν υπάρχουν ακόμη αποτελέσματα")
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Started.After(all[j].Started) })
	return all[0], nil
}
func (s *Store) Approve(id string) (string, error) {
	p, _, e := s.Get(id)
	if e != nil {
		return "", e
	}
	run, e := s.LatestRun(id)
	if e != nil {
		return "", e
	}
	current, e := HashTree(filepath.Join(s.Root, "projects", id, "source"))
	if e != nil {
		return "", e
	}
	if run.SourceHash != current {
		return "", errors.New("Ο κώδικας άλλαξε μετά τα τεστ — απαιτούνται νέοι έλεγχοι")
	}
	if IsBlocked(run) {
		return "", errors.New("Αποκλείεται η σταθερή έκδοση: αποτυχία ή έλλειψη υποχρεωτικών τεστ")
	}
	root := filepath.Join(s.Root, "projects", id)
	dst := filepath.Join(root, "stable", run.ID)
	if exists(dst) {
		return dst, nil
	}
	stage := dst + ".new"
	os.RemoveAll(stage)
	if e = copyTree(filepath.Join(root, "source"), filepath.Join(stage, "source")); e != nil {
		os.RemoveAll(stage)
		return "", e
	}
	if exists(filepath.Join(root, "runs", run.ID, "artifacts")) {
		if e = copyTree(filepath.Join(root, "runs", run.ID, "artifacts"), filepath.Join(stage, "artifacts")); e != nil {
			os.RemoveAll(stage)
			return "", e
		}
	}
	verifiedHash, verr := HashTree(filepath.Join(stage, "source"))
	if verr != nil || verifiedHash != run.SourceHash {
		os.RemoveAll(stage)
		return "", errors.New("Αποτυχία επαλήθευσης πηγών σταθερής έκδοσης")
	}
	if e = atomicJSON(filepath.Join(stage, "approval.json"), map[string]any{"project": p, "run": run}); e != nil {
		os.RemoveAll(stage)
		return "", e
	}
	if e = os.Rename(stage, dst); e != nil {
		os.RemoveAll(stage)
		return "", e
	}
	return dst, nil
}
func (s *Store) Backup(id string) (string, error) {
	p, _, e := s.Get(id)
	if e != nil {
		return "", e
	}
	src := filepath.Join(s.Root, "projects", id, "source")
	h, e := HashTree(src)
	if e != nil {
		return "", e
	}
	dst := filepath.Join(s.Root, "backups", id+"-"+newID())
	if e = copyTree(src, filepath.Join(dst, "source")); e != nil {
		os.RemoveAll(dst)
		return "", e
	}
	verify, verr := HashTree(filepath.Join(dst, "source"))
	if verr != nil || verify != h {
		os.RemoveAll(dst)
		return "", errors.New("Αποτυχία επαλήθευσης ακεραιότητας αντιγράφου")
	}
	if e = atomicJSON(filepath.Join(dst, "backup.json"), map[string]any{"name": p.Name, "version": p.Version, "language": p.Language, "hash": h, "created": time.Now()}); e != nil {
		return "", e
	}
	return dst, nil
}
func (s *Store) RestoreNewest(id string) (Project, error) {
	var result Project
	matches, e := filepath.Glob(filepath.Join(s.Root, "backups", id+"-*"))
	if e != nil {
		return result, e
	}
	if len(matches) == 0 {
		return result, errors.New("Δεν υπάρχει αντίγραφο για το έργο")
	}
	sort.Slice(matches, func(i, j int) bool {
		a, _ := os.Stat(matches[i])
		b, _ := os.Stat(matches[j])
		return a.ModTime().After(b.ModTime())
	})
	meta, e := os.ReadFile(filepath.Join(matches[0], "backup.json"))
	if e != nil {
		return result, e
	}
	var v struct{ Name, Version, Language, Hash string }
	if e = json.Unmarshal(meta, &v); e != nil {
		return result, e
	}
	got, e := HashTree(filepath.Join(matches[0], "source"))
	if e != nil {
		return result, e
	}
	if got != v.Hash {
		return result, errors.New("Το αντίγραφο ασφαλείας αλλοιώθηκε")
	}
	result = Project{ID: newID(), Name: v.Name + " (επαναφορά)", Version: v.Version, Language: v.Language, SourceHash: got, ImportedAt: time.Now()}
	dst := filepath.Join(s.Root, "projects", result.ID)
	stage := dst + ".new"
	if e = copyTree(filepath.Join(matches[0], "source"), filepath.Join(stage, "source")); e != nil {
		os.RemoveAll(stage)
		return result, e
	}
	if e = atomicJSON(filepath.Join(stage, "project.json"), result); e != nil {
		os.RemoveAll(stage)
		return result, e
	}
	if e = os.Rename(stage, dst); e != nil {
		os.RemoveAll(stage)
		return result, e
	}
	return result, nil
}
func (s *Store) ExportLatest(id string) (string, error) {
	_, _, e := s.Get(id)
	if e != nil {
		return "", e
	}
	r, e := s.LatestRun(id)
	if e != nil {
		return "", e
	}
	dir := filepath.Join(s.Root, "projects", id, "runs", r.ID)
	out := filepath.Join(s.Root, "reports", id+"-"+r.ID+"-RESULTS.zip")
	entries := map[string][]byte{}
	for _, name := range []string{"SUMMARY_GR.txt", "results.json"} {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			return "", e
		}
		entries[name] = b
	}
	env, _ := json.MarshalIndent(map[string]any{"host_os": r.HostOS, "app_version": ProductVersion, "run_id": r.ID, "source_sha256": r.SourceHash}, "", "  ")
	entries["environment.json"] = env
	artifacts, _ := json.MarshalIndent(r.Artifacts, "", "  ")
	entries["artifacts/manifest.json"] = artifacts
	var junit strings.Builder
	junit.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<testsuite name=\"Κέντρο Προγραμματισμού\">\n")
	for _, v := range r.Results {
		junit.WriteString("<testcase name=\"")
		xml.EscapeText(&junit, []byte(v.Phase+" - "+v.Name))
		junit.WriteString("\">")
		if v.Status == "FAIL" {
			junit.WriteString("<failure>")
			xml.EscapeText(&junit, []byte(v.Reason))
			junit.WriteString("</failure>")
		}
		if v.Status == "NOT RUN" || v.Status == "CANCELLED" {
			junit.WriteString("<skipped>")
			xml.EscapeText(&junit, []byte(v.Reason))
			junit.WriteString("</skipped>")
		}
		junit.WriteString("</testcase>\n")
	}
	junit.WriteString("</testsuite>\n")
	entries["results.junit.xml"] = []byte(junit.String())
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var sums strings.Builder
	for _, name := range names {
		h := sha256.Sum256(entries[name])
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(h[:]), name)
	}
	entries["checksums.sha256"] = []byte(sums.String())
	names = append(names, "checksums.sha256")
	f, e := os.Create(out)
	if e != nil {
		return "", e
	}
	z := zip.NewWriter(f)
	for _, name := range names {
		w, e := z.Create(name)
		if e != nil {
			z.Close()
			f.Close()
			return "", e
		}
		if _, e = w.Write(entries[name]); e != nil {
			z.Close()
			f.Close()
			return "", e
		}
	}
	if e = z.Close(); e != nil {
		f.Close()
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	return out, nil
}
