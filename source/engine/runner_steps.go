package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

func missingBuildRequirement(ctx context.Context, work string, step Step) string {
	if strings.EqualFold(step.Command, "python") && len(step.Args) >= 2 && step.Args[0] == "-m" && step.Args[1] == "PyInstaller" {
		python, err := exec.LookPath("python")
		if err != nil {
			return "Δεν είναι διαθέσιμη η Python στον υπολογιστή"
		}
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(check, python, "-c", "import importlib.util,sys;sys.exit(0 if importlib.util.find_spec('PyInstaller') else 5)")
		hideToolConsole(cmd)
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
		killer := exec.CommandContext(ctx, "taskkill.exe", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid))
		hideToolConsole(killer)
		_ = killer.Run()
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
	hideToolConsole(cmd)
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
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			r.Status = "CANCELLED"
			r.FailureKind = "CANCELLED"
			r.Reason = "Ο έλεγχος ακυρώθηκε από τον χειριστή"
		case errors.Is(ctx2.Err(), context.DeadlineExceeded):
			r.FailureKind = "TIMEOUT"
			r.Reason = fmt.Sprintf("Υπέρβαση χρονικού ορίου %d δευτερολέπτων", sec)
		default:
			r.FailureKind = "COMMAND_ERROR"
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
