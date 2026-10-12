package engine

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if _, e := os.Stat("/usr/local/go/bin/go"); e != nil {
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
	if !run.Blocked {
		t.Fatal("Non-Windows build approved")
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
	if !r.Blocked {
		t.Fatal("Linux run cannot approve Windows exe")
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
