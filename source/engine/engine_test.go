package engine

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func zipFixture(t *testing.T, files map[string]string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "example.zip")
	f, e := os.Create(out)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(f)
	for name, content := range files {
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		w.Write([]byte(content))
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	f.Close()
	return out
}
func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, e := NewStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestImportRejectTraversal(t *testing.T) {
	for _, path := range []string{"../bad.txt", "/bad.txt", "a/../../bad.txt", "C:/oops", "folder\\test", "CON.txt", "a/../b.txt", "a//b.txt"} {
		t.Run(path, func(t *testing.T) {
			s := newTestStore(t)
			if _, e := s.ImportZIP(zipFixture(t, map[string]string{path: "oops"})); e == nil {
				t.Fatalf("Unsafe path accepted: %q", path)
			}
			items, _ := s.List()
			if len(items) != 0 {
				t.Fatal("Partial import left behind")
			}
		})
	}
}
func TestImportDuplicateCase(t *testing.T) {
	s := newTestStore(t)
	if _, e := s.ImportZIP(zipFixture(t, map[string]string{"Foo.txt": "1", "foo.txt": "2"})); e == nil {
		t.Fatal("Duplicate names accepted")
	}
}
func TestImportChecksum(t *testing.T) {
	s := newTestStore(t)
	sum := sha256.Sum256([]byte("hello"))
	z := zipFixture(t, map[string]string{"hi.txt": "hello", "CHECKSUMS.sha256": hex.EncodeToString(sum[:]) + "  hi.txt\n"})
	p, e := s.ImportZIP(z)
	if e != nil {
		t.Fatal(e)
	}
	if p.SourceHash == "" {
		t.Fatal("Missing hash")
	}
	z = zipFixture(t, map[string]string{"hi.txt": "wrong", "CHECKSUMS.sha256": hex.EncodeToString(sum[:]) + "  hi.txt\n"})
	if _, e = s.ImportZIP(z); e == nil {
		t.Fatal("Invalid hash accepted")
	}
}
func TestImportManifestAndRollback(t *testing.T) {
	s := newTestStore(t)
	m := Manifest{Name: "Δοκιμαστικό", Version: "2.0", Language: "Go", Tests: []Step{{Name: "Test", Command: "go", Args: []string{"version"}}}}
	mb, _ := json.Marshal(m)
	p, e := s.ImportZIP(zipFixture(t, map[string]string{"project-manifest.json": string(mb), "go.mod": "module example.local/demo\n\ngo 1.23\n"}))
	if e != nil {
		t.Fatal(e)
	}
	ps, e := s.List()
	if e != nil || len(ps) != 1 {
		t.Fatalf("List: %v %v", ps, e)
	}
	_, got, e := s.Get(p.ID)
	if e != nil || got.Version != "2.0" {
		t.Fatalf("Get: %v %v", got, e)
	}
	if _, e = s.Approve(p.ID); e == nil {
		t.Fatal("Approved without tests")
	}
	if _, e = s.Backup(p.ID); e != nil {
		t.Fatal(e)
	}
	restored, e := s.RestoreNewest(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if restored.ID == p.ID || restored.SourceHash != p.SourceHash {
		t.Fatal("Restore did not create separate verified project")
	}
}
func TestMandatoryGate(t *testing.T) {
	ok := Run{HostOS: "windows", Results: []Result{{Phase: "TEST", Required: true, Status: "PASS"}, {Phase: "STRESS", Required: true, Status: "PASS"}, {Phase: "BUILD", Required: true, Status: "PASS"}, {Phase: "SMOKE", Required: true, Status: "PASS"}, {Phase: "INTEGRITY", Required: true, Status: "PASS"}}}
	if IsBlocked(ok) {
		t.Fatal("All-pass blocked")
	}
	for _, st := range []string{"FAIL", "NOT RUN", "CANCELLED"} {
		x := ok
		x.Results = append([]Result{}, ok.Results...)
		x.Results[0].Status = st
		if !IsBlocked(x) {
			t.Errorf("Mandatory %s accepted", st)
		}
	}
	x := ok
	x.HostOS = "linux"
	if !IsBlocked(x) {
		t.Fatal("Non-Windows run accepted")
	}
}
func TestNoTestsNotApproved(t *testing.T) {
	s := newTestStore(t)
	p, e := s.ImportZIP(zipFixture(t, map[string]string{"README.txt": "no project"}))
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.RunAll(context.Background(), p.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	if !r.Blocked {
		t.Fatal("Empty project accepted")
	}
	found := false
	for _, v := range r.Results {
		if v.Status == "NOT RUN" && v.Required {
			found = true
		}
	}
	if !found {
		t.Fatal("Missing NOT RUN")
	}
	if _, e = s.Approve(p.ID); e == nil {
		t.Fatal("Approved empty project")
	}
	f, e := s.ExportLatest(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(f); e != nil {
		t.Fatal(e)
	}
}
func TestOfflineGoTests(t *testing.T) {
	if _, e := exec.LookPath("go"); e != nil {
		t.Skip("No local Go compiler")
	}
	s := newTestStore(t)
	m := Manifest{Name: "sample", Version: "0.1", Tests: []Step{{Name: "Pass", Command: "go", Args: []string{"test", "./..."}, TimeoutSeconds: 40}}, Stress: []StressSpec{{Name: "Load", Kind: "parallel", Command: "go", Args: []string{"test", "./..."}, Parallel: 2, Repeats: 3, TimeoutSeconds: 40}}, Build: &Step{Name: "Build", Command: "go", Args: []string{"build", "-o", "output.exe", "."}}, Smoke: &Step{Name: "Smoke", Command: "./output.exe"}}
	b, _ := json.Marshal(m)
	p, e := s.ImportZIP(zipFixture(t, map[string]string{"go.mod": "module example.local/sample\n\ngo 1.23\n", "main.go": "package main\nfunc main(){}\n", "main_test.go": "package main\nimport \"testing\"\nfunc TestIt(t *testing.T){}\n", "project-manifest.json": string(b)}))
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.RunAll(context.Background(), p.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	pass := 0
	for _, r := range run.Results {
		if r.Status == "PASS" {
			pass++
		} else if r.Phase != "BUILD" && r.Phase != "SMOKE" {
			t.Errorf("Unexpected step: %+v", r)
		}
	}
	if pass < 3 {
		t.Fatalf("Expected test, stress, integrity PASS: %+v", run.Results)
	}
	if runtime.GOOS != "windows" && !run.Blocked {
		t.Fatal("Non-Windows build approved")
	}
	if runtime.GOOS == "windows" && run.Blocked {
		t.Fatalf("Windows run unexpectedly blocked: %+v", run.Results)
	}
	root, _ := s.Base(p.ID)
	if e = os.WriteFile(filepath.Join(root, "source", "main.go"), []byte("package main\nfunc main(){println(1)}\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Approve(p.ID); e == nil || !strings.Contains(e.Error(), "άλλαξε") {
		t.Fatalf("Expected hash guard; got %v", e)
	}
}

func TestExampleFullPipeline(t *testing.T) {
	s := newTestStore(t)
	file := filepath.Join("..", "..", "examples", "ΔΟΚΙΜΗ_ΕΡΓΟΥ_GO.zip")
	p, e := s.ImportZIP(file)
	if e != nil {
		t.Fatal(e)
	}
	_, m, e := s.Get(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if m.WorkDir != "SOURCE" || p.Language != "Go" {
		t.Fatalf("Bad package recognition: %+v %+v", p, m)
	}
	r, e := s.RunAll(context.Background(), p.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]string{}
	for _, result := range r.Results {
		seen[result.Phase] = result.Status
	}
	if seen["TEST"] != "PASS" || seen["STRESS"] != "PASS" || seen["INTEGRITY"] != "PASS" {
		t.Fatalf("Functional/stress test failed: %+v", r.Results)
	}
	if runtime.GOOS != "windows" && !r.Blocked {
		t.Fatal("Non-Windows run cannot approve Windows exe")
	}
	if runtime.GOOS == "windows" && r.Blocked {
		t.Fatalf("Windows pipeline unexpectedly blocked: %+v", r.Results)
	}
	out, e := s.ExportLatest(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	z, e := zip.OpenReader(out)
	if e != nil {
		t.Fatal(e)
	}
	defer z.Close()
	needed := map[string]bool{"results.json": false, "SUMMARY_GR.txt": false, "checksums.sha256": false, "results.junit.xml": false, "artifacts/manifest.json": false}
	for _, f := range z.File {
		if _, ok := needed[f.Name]; ok {
			needed[f.Name] = true
		}
	}
	for name, ok := range needed {
		if !ok {
			t.Errorf("Report missing: %s", name)
		}
	}
}

func TestMissingStressToolIsNotRun(t *testing.T) {
	// A computer with no Go toolchain must report missing prerequisites, not
	// fabricated failed stress iterations.
	t.Setenv("PATH", "")
	r := runStress(context.Background(), t.TempDir(), t.TempDir(), 0, StressSpec{
		Name: "Parallel checks", Kind: "parallel", Command: "go", Parallel: 3, Repeats: 5,
	})
	if r.Status != "NOT RUN" || !strings.Contains(r.Reason, "go") || r.DurationMS != 0 {
		t.Fatalf("Missing Go incorrectly reported as %s: %+v", r.Status, r)
	}
}

// Test the repository shape used by Python applications such as Position Tracking.
func TestAutoManifestPythonScriptDiscovery(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "tests"), 0700); err != nil {
		t.Fatal(err)
	}
	for name := range map[string]bool{"requirements.txt": true, "tests/network_resilience_regression.py": true, "tests/offline_15_clients_restart_stress.py": true, "tests/__init__.py": true} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.WriteFile(p, []byte("print('PASS')\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := AutoManifest(root)
	if m.Language != "Python" || len(m.Tests) != 1 || len(m.Stress) != 1 {
		t.Fatalf("unexpected Python plan: lang=%s tests=%d stress=%d", m.Language, len(m.Tests), len(m.Stress))
	}
	if e := ValidateManifest(m); e != nil {
		t.Fatal(e)
	}
	if m.Build != nil {
		t.Fatal("must not invent Python EXE build configuration")
	}
}

// This fixture resembles a conventional desktop Python project but contains
// no agency names, locations or operational data.
func TestPythonWorkflowBuildPlanAndVisualOrdering(t *testing.T) {
	s := newTestStore(t)
	workflow := `name: Windows
steps:
  - name: Build application
    run: |
      python -m PyInstaller --noconfirm --clean --windowed --onefile --name DemoDesk src/demo.py
`
	files := map[string]string{
		".github/workflows/windows.yml":              workflow,
		"src/demo.py":                                "if __name__ == '__main__':\n    print('ok')\n",
		"requirements.txt":                           "PyInstaller>=6\n",
		"tests/test_logic.py":                        "print('TEST PASSED')\n",
		"tests/offline_15_clients_restart_stress.py": "print('STRESS PASSED')\n",
		"tests/visual_flicker_windows.py":            "print('Requires --exe and --out')\n",
	}
	p, e := s.ImportZIP(zipFixture(t, files))
	if e != nil {
		t.Fatal(e)
	}
	_, m, e := s.Get(p.ID)
	if e != nil {
		t.Fatal(e)
	}
	if len(m.Tests) != 1 || len(m.Stress) != 1 || len(m.Visual) != 1 {
		t.Fatalf("Wrong phases: tests=%d stress=%d visual=%d", len(m.Tests), len(m.Stress), len(m.Visual))
	}
	if m.Build == nil || m.Smoke == nil {
		t.Fatalf("Lost Python Windows build recipe: %+v", m)
	}
	if m.Build.Command != "python" || !strings.Contains(strings.Join(m.Build.Args, " "), "--name DemoDesk src/demo.py") {
		t.Fatalf("Unexpected build recipe: %+v", m.Build)
	}
	if !strings.Contains(strings.Join(m.Visual[0].Args, " "), "--exe dist/DemoDesk.exe --out visual_checks") {
		t.Fatalf("Visual step lacks EXE args: %+v", m.Visual[0])
	}
	if e := ValidateManifest(m); e != nil {
		t.Fatal(e)
	}
}
func TestPythonVisualWithoutBuildNeverRunsAsOrdinaryTest(t *testing.T) {
	root := t.TempDir()
	if e := os.MkdirAll(filepath.Join(root, "tests"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "src.py"), []byte("print('ok')\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "tests", "visual_flicker_windows.py"), []byte("raise RuntimeError('should not run')\n"), 0600); e != nil {
		t.Fatal(e)
	}
	m := AutoManifest(root)
	if len(m.Tests) != 0 || len(m.Visual) != 0 || m.Build != nil {
		t.Fatalf("Unsafe autodiscovery: %+v", m)
	}
}
func TestVisualReportFailClosed(t *testing.T) {
	root := t.TempDir()
	r := checkVisualResults(root, Result{Status: "PASS", Phase: "VISUAL"})
	if r.Status != "FAIL" {
		t.Fatalf("Accepted nonexistent visual evidence: %+v", r)
	}
	if e := os.MkdirAll(filepath.Join(root, "visual_checks"), 0700); e != nil {
		t.Fatal(e)
	}
	report := `{"startup_endpoint_flicker_runs":1,"theme_endpoint_flicker_toggles":0,"theme_clicks_confirmed":30,"startup_runs":[{}],"theme_toggles":[{}]}`
	if e := os.WriteFile(filepath.Join(root, "visual_checks", "report.json"), []byte(report), 0600); e != nil {
		t.Fatal(e)
	}
	r = checkVisualResults(root, Result{Status: "PASS", Phase: "VISUAL"})
	if r.Status != "FAIL" {
		t.Fatalf("Accepted measured flicker: %+v", r)
	}
	report = `{"startup_endpoint_flicker_runs":0,"theme_endpoint_flicker_toggles":0,"theme_clicks_confirmed":30,"startup_runs":[{}],"theme_toggles":[{}]}`
	if e := os.WriteFile(filepath.Join(root, "visual_checks", "report.json"), []byte(report), 0600); e != nil {
		t.Fatal(e)
	}
	r = checkVisualResults(root, Result{Status: "PASS", Phase: "VISUAL"})
	if r.Status != "PASS" {
		t.Fatalf("Rejected normal evidence: %+v", r)
	}
}
func TestMissingBuildDoesNotDownload(t *testing.T) {
	ctx := context.Background()
	m := Manifest{Language: "Python", Build: &Step{Name: "build", Command: "python", Args: []string{"-m", "PyInstaller", "--clean", "main.py"}}}
	if len(Readiness(m)) == 0 {
		t.Fatal("No tool-readiness report")
	}
	t.Setenv("PATH", "")
	msg := missingBuildRequirement(ctx, t.TempDir(), *m.Build)
	if !strings.Contains(msg, "Python") {
		t.Fatalf("Expected missing local Python, got %q", msg)
	}
}

// This opt-in full-stack test uses an entirely synthetic Tkinter GUI, executes
// Python tests and stress, builds a real Windows EXE with PyInstaller, and
// verifies that the EXE stays running. It must run on GitHub Windows CI where
// PyInstaller has been provisioned; it is not a background network installer.
func TestPythonBuildEndToEndWindows(t *testing.T) {
	if runtime.GOOS != "windows" || os.Getenv("KENTRO_RUN_PY_BUILD_TEST") != "1" {
		t.Skip("Explicit Windows CI integration test only")
	}
	if _, e := exec.LookPath("python"); e != nil {
		t.Fatal("Python not available on integration runner")
	}
	s := newTestStore(t)
	m := Manifest{Name: "DemoDesk", Version: "0.1", Language: "Python",
		Tests:  []Step{{Name: "Python unit", Command: "python", Args: []string{"tests/test_demo.py"}, TimeoutSeconds: 60}},
		Stress: []StressSpec{{Name: "Parallel Python", Kind: "parallel", Command: "python", Args: []string{"tests/stress_demo.py"}, Parallel: 2, Repeats: 3, TimeoutSeconds: 60}},
		Build:  &Step{Name: "Windows Python EXE", Command: "python", Args: []string{"-m", "PyInstaller", "--noconfirm", "--clean", "--onefile", "--windowed", "--name", "DemoDesk", "src/demo.py"}, TimeoutSeconds: 600},
		Smoke:  &Step{Name: "GUI remains open", Command: "./dist/DemoDesk.exe", ExpectContains: "WINDOW_STAYS_OPEN", TimeoutSeconds: 30},
	}
	b, _ := json.Marshal(m)
	files := map[string]string{
		"project-manifest.json": string(b),
		"src/demo.py":           "import tkinter as tk\nroot = tk.Tk()\nroot.title('DemoDesk')\nroot.after(12000, root.destroy)\nroot.mainloop()\n",
		"tests/test_demo.py":    "assert 2+2 == 4\n",
		"tests/stress_demo.py":  "import threading\nxs=[]\nthreads=[threading.Thread(target=lambda:xs.append(1)) for _ in range(20)]\nfor thread in threads:thread.start()\nfor thread in threads:thread.join()\nassert len(xs)==20\n",
	}
	p, e := s.ImportZIP(zipFixture(t, files))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	result, e := s.RunAll(ctx, p.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	for _, step := range result.Results {
		if step.Required && step.Status != "PASS" {
			t.Errorf("Required stage %s: %s - %s", step.Phase, step.Status, step.Reason)
		}
	}
	if result.Blocked || len(result.Artifacts) == 0 {
		t.Fatalf("Synthetic Windows/Python build not approved: %+v", result)
	}
	if _, e = s.Approve(p.ID); e != nil {
		t.Fatal(e)
	}
}
