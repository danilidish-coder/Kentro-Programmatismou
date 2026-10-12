package engine

import (
	"archive/zip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const maxFiles = 5000
const maxFile = 64 << 20
const maxTotal = 512 << 20

func newID() string {
	b := make([]byte, 12)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func zipPathOK(name string) bool {
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, ":") || strings.ContainsRune(name, 0) {
		return false
	}
	c := path.Clean(name)
	if c == "." || c != strings.TrimSuffix(name, "/") && c+"/" != name || c == ".." || strings.HasPrefix(c, "../") {
		return false
	}
	for _, component := range strings.Split(c, "/") {
		if component == "" || component == "." || component == ".." || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return false
		}
		u := strings.ToUpper(strings.SplitN(component, ".", 2)[0])
		switch u {
		case "CON", "PRN", "AUX", "NUL":
			return false
		}
		if len(u) == 4 && (strings.HasPrefix(u, "COM") || strings.HasPrefix(u, "LPT")) && u[3] >= '1' && u[3] <= '9' {
			return false
		}
	}
	return true
}

type Preview struct {
	Files            int
	SizeBytes        uint64
	ContainsManifest bool
}

func PreviewZIP(file string) (Preview, error) {
	var v Preview
	z, e := zip.OpenReader(file)
	if e != nil {
		return v, e
	}
	defer z.Close()
	if len(z.File) == 0 || len(z.File) > maxFiles {
		return v, errors.New("Μη αποδεκτός αριθμός αρχείων")
	}
	seen := map[string]bool{}
	for _, f := range z.File {
		if !zipPathOK(f.Name) {
			return v, fmt.Errorf("Μη ασφαλής διαδρομή: %s", f.Name)
		}
		key := strings.ToLower(strings.TrimSuffix(f.Name, "/"))
		if seen[key] {
			return v, errors.New("Διπλό αρχείο: " + key)
		}
		seen[key] = true
		if f.Mode()&os.ModeSymlink != 0 || f.Mode()&os.ModeType != 0 && !f.FileInfo().IsDir() {
			return v, errors.New("Μη επιτρεπτά ειδικά αρχεία")
		}
		if f.UncompressedSize64 > maxFile {
			return v, errors.New("Υπερβολικά μεγάλο αρχείο")
		}
		v.SizeBytes += f.UncompressedSize64
		if v.SizeBytes > maxTotal {
			return v, errors.New("Υπερβολικό συνολικό μέγεθος")
		}
		if f.UncompressedSize64 > 1<<20 && f.CompressedSize64 > 0 && f.UncompressedSize64/f.CompressedSize64 > 200 {
			return v, errors.New("Ύποπτη συμπίεση")
		}
		if !f.FileInfo().IsDir() {
			v.Files++
		}
		if strings.HasSuffix(strings.ToLower(f.Name), "project-manifest.json") {
			v.ContainsManifest = true
		}
	}
	return v, nil
}

func (s *Store) ImportZIP(file string) (Project, error) {
	var p Project
	z, e := zip.OpenReader(file)
	if e != nil {
		return p, fmt.Errorf("Αποτυχία ανάγνωσης ZIP: %w", e)
	}
	defer z.Close()
	if len(z.File) == 0 || len(z.File) > maxFiles {
		return p, errors.New("Μη αποδεκτός αριθμός αρχείων ZIP")
	}
	id := newID()
	root := filepath.Join(s.Root, "projects", id)
	stage := root + ".new"
	if e = os.MkdirAll(filepath.Join(stage, "source"), 0700); e != nil {
		return p, e
	}
	defer os.RemoveAll(stage)
	seen := map[string]bool{}
	var total uint64
	files := 0
	for _, f := range z.File {
		if !zipPathOK(f.Name) {
			return p, fmt.Errorf("Μη ασφαλής διαδρομή ZIP: %q", f.Name)
		}
		nm := strings.TrimSuffix(f.Name, "/")
		key := strings.ToLower(nm)
		if seen[key] {
			return p, errors.New("Διπλή διαδρομή ZIP: " + nm)
		}
		seen[key] = true
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 || mode&os.ModeType != 0 && !f.FileInfo().IsDir() {
			return p, errors.New("Δεν επιτρέπονται σύνδεσμοι ή ειδικά αρχεία")
		}
		if f.UncompressedSize64 > maxFile {
			return p, errors.New("Υπερβολικά μεγάλο αρχείο στο ZIP")
		}
		total += f.UncompressedSize64
		if total > maxTotal {
			return p, errors.New("Υπέρβαση συνολικού μεγέθους ZIP")
		}
		if f.UncompressedSize64 > 1<<20 && f.CompressedSize64 > 0 && f.UncompressedSize64/f.CompressedSize64 > 200 {
			return p, errors.New("Ύποπτος λόγος συμπίεσης")
		}
		dest := filepath.Join(stage, "source", filepath.FromSlash(nm))
		if f.FileInfo().IsDir() {
			if e = os.MkdirAll(dest, 0700); e != nil {
				return p, e
			}
			continue
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return p, e
		}
		r, e := f.Open()
		if e != nil {
			return p, e
		}
		w, e := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			r.Close()
			return p, e
		}
		n, ce := io.Copy(w, io.LimitReader(r, maxFile+1))
		e1 := w.Close()
		e2 := r.Close()
		if ce != nil {
			return p, ce
		}
		if e1 != nil {
			return p, e1
		}
		if e2 != nil {
			return p, e2
		}
		if n != int64(f.UncompressedSize64) {
			return p, errors.New("Λανθασμένο μέγεθος αρχείου ZIP")
		}
		files++
	}
	if files == 0 {
		return p, errors.New("Το ZIP δεν περιέχει αρχεία")
	}
	source := filepath.Join(stage, "source")
	// Standard ZIP exports frequently contain a single enclosing project folder.
	entries, readErr := os.ReadDir(source)
	if readErr != nil {
		return p, readErr
	}
	if len(entries) == 1 && entries[0].IsDir() {
		inner := filepath.Join(source, entries[0].Name())
		flat := filepath.Join(stage, "flat-source")
		if e = os.Rename(inner, flat); e != nil {
			return p, e
		}
		if e = os.Remove(source); e != nil {
			return p, e
		}
		if e = os.Rename(flat, source); e != nil {
			return p, e
		}
	}
	if e = VerifyChecksums(source); e != nil {
		return p, e
	}
	m := AutoManifest(source)
	if b, err := os.ReadFile(filepath.Join(source, "project-manifest.json")); err == nil {
		if err = json.Unmarshal(b, &m); err != nil {
			return p, fmt.Errorf("Λάθος manifest: %w", err)
		}
		if e = ValidateManifest(m); e != nil {
			return p, e
		}
	}
	hash, e := HashTree(source)
	if e != nil {
		return p, e
	}
	title := m.Name
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	}
	p = Project{ID: id, Name: title, Version: m.Version, Language: DetectLanguage(source), SourceHash: hash, ImportedAt: time.Now()}
	if e = atomicJSON(filepath.Join(stage, "project.json"), p); e != nil {
		return p, e
	}
	if e = os.Rename(stage, root); e != nil {
		return p, e
	}
	return p, nil
}
func VerifyChecksums(root string) error {
	b, e := os.ReadFile(filepath.Join(root, "CHECKSUMS.sha256"))
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) < 67 || line[64:66] != "  " {
			return errors.New("Λάθος γραμμή CHECKSUMS.sha256")
		}
		h := line[:64]
		if _, e = hex.DecodeString(h); e != nil {
			return e
		}
		name := line[66:]
		if !zipPathOK(name) || seen[strings.ToLower(name)] {
			return errors.New("Μη αποδεκτή εγγραφή SHA256")
		}
		seen[strings.ToLower(name)] = true
		data, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if e != nil {
			return e
		}
		actual := sha256.Sum256(data)
		if !strings.EqualFold(h, hex.EncodeToString(actual[:])) {
			return fmt.Errorf("Διαφορετικό SHA256: %s", name)
		}
	}
	return nil
}
func HashTree(root string) (string, error) {
	files := []string{}
	e := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Symbolic link not allowed")
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return errors.New("Special file not allowed")
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if e != nil {
		return "", e
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		io.WriteString(h, f+"\x00")
		b, e := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if e != nil {
			return "", e
		}
		d := sha256.Sum256(b)
		h.Write(d[:])
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func DetectLanguage(root string) string {
	if exists(filepath.Join(root, "SOURCE", "go.mod")) {
		return "Go"
	}
	if exists(filepath.Join(root, "SOURCE", "pyproject.toml")) {
		return "Python"
	}
	if exists(filepath.Join(root, "go.mod")) {
		return "Go"
	}
	if exists(filepath.Join(root, "pyproject.toml")) || exists(filepath.Join(root, "requirements.txt")) {
		return "Python"
	}
	if exists(filepath.Join(root, "package.json")) {
		return "JavaScript"
	}
	if exists(filepath.Join(root, "Cargo.toml")) {
		return "Rust"
	}
	lang := "Άγνωστη"
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == ".venv" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(info.Name()))
		switch ext {
		case ".py":
			lang = "Python"
			return io.EOF
		case ".csproj", ".sln":
			lang = "C#"
			return io.EOF
		case ".js", ".ts":
			if lang == "Άγνωστη" {
				lang = "JavaScript"
			}
		}
		return nil
	})
	return lang
}
func AutoManifest(root string) Manifest {
	m := Manifest{Name: filepath.Base(root), Language: DetectLanguage(root)}
	// Import is read-only. These commands are offered as a plan and executed
	// only after an explicit user confirmation in the GUI.
	if m.Language == "Go" && hasTestFiles(root, "_test.go") {
		m.Tests = []Step{{Name: "Go unit tests", Command: "go", Args: []string{"test", "./..."}, TimeoutSeconds: 180}}
		m.Build = &Step{Name: "Windows EXE", Command: "go", Args: []string{"build", "-o", "output.exe", "."}, TimeoutSeconds: 180}
	}
	if m.Language == "Python" {
		// An explicit manifest always wins. If absent, infer a conservative,
		// reproducible PyInstaller recipe only when a trustworthy source path
		// and distinct EXE name can be derived from local project files.
		exeName, entry := detectPythonBuild(root)
		if entry != "" {
			args := []string{"-m", "PyInstaller", "--noconfirm", "--clean", "--onefile", "--windowed", "--name", exeName, entry}
			if strings.HasSuffix(entry, ".spec") {
				args = []string{"-m", "PyInstaller", "--noconfirm", "--clean", entry}
			}
			m.Build = &Step{Name: "Κατασκευή Windows EXE (Python)", Command: "python", Args: args, TimeoutSeconds: 900}
			m.Smoke = &Step{Name: "Έλεγχος πραγματικής εκκίνησης", Command: "./dist/" + exeName + ".exe", ExpectContains: "WINDOW_STAYS_OPEN", TimeoutSeconds: 30}
		}
		scripts := []string{}
		testsDir := filepath.Join(root, "tests")
		// Python regression scripts are often standalone scripts and are not
		// always discoverable by pytest's default naming rules.
		_ = filepath.Walk(testsDir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info == nil {
				return nil
			}
			if info.IsDir() {
				if info.Name() == "__pycache__" {
					return filepath.SkipDir
				}
				return nil
			}
			name := strings.ToLower(info.Name())
			if !strings.HasSuffix(name, ".py") || strings.HasPrefix(name, "__") {
				return nil
			}
			rel, e := filepath.Rel(root, p)
			if e != nil {
				return nil
			}
			scripts = append(scripts, filepath.ToSlash(rel))
			return nil
		})
		sort.Strings(scripts)
		for _, script := range scripts {
			name := strings.ToLower(filepath.Base(script))
			if name == "visual_flicker_windows.py" || strings.HasPrefix(name, "visual_") || strings.Contains(name, "screenshot") {
				if m.Smoke != nil && name == "visual_flicker_windows.py" {
					m.Visual = append(m.Visual, Step{Name: "Έλεγχος εικόνας και τρεμοπαίγματος", Command: "python", Args: []string{script, "--exe", "dist/" + exeName + ".exe", "--out", "visual_checks"}, TimeoutSeconds: 900})
				}
				// Do not execute GUI-capture scripts as ordinary tests.
				continue
			}
			if strings.Contains(name, "stress") || strings.Contains(name, "load") {
				if len(m.Stress) < 30 {
					// The script itself controls load; don't multiply it again.
					m.Stress = append(m.Stress, StressSpec{Name: "Σενάριο πίεσης: " + filepath.Base(script), Kind: "parallel", Command: "python", Args: []string{script}, Parallel: 1, Repeats: 1, TimeoutSeconds: 120})
				}
			} else if len(m.Tests) < 100 {
				m.Tests = append(m.Tests, Step{Name: "Έλεγχος: " + filepath.Base(script), Command: "python", Args: []string{script}, TimeoutSeconds: 180})
			}
		}
	}
	return m
}

// detectPythonBuild never executes YAML and never downloads anything. It only
// recognizes a minimal known-safe PyInstaller command from the project's
// existing workflow, or a single pre-existing .spec file.
func detectPythonBuild(root string) (string, string) {
	specs, _ := filepath.Glob(filepath.Join(root, "*.spec"))
	if len(specs) == 1 {
		// Reusing a .spec requires no inference about program entry points.
		spec := filepath.Base(specs[0])
		name := strings.TrimSuffix(spec, ".spec")
		if safeExeName(name) {
			return name, spec
		}
	}
	workflowFiles, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yml"))
	more, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.yaml"))
	workflowFiles = append(workflowFiles, more...)
	for _, wf := range workflowFiles {
		data, err := os.ReadFile(wf)
		if err != nil || len(data) > 1000000 {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			// Deliberately ignore shell interpolation and pipelines; do not
			// treat arbitrary user-supplied YAML as an executable recipe.
			if strings.ContainsAny(line, "`$;|&") {
				continue
			}
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) < 6 || !strings.EqualFold(fields[0], "python") || fields[1] != "-m" || !strings.EqualFold(fields[2], "PyInstaller") {
				continue
			}
			var name, entry string
			for i := 3; i < len(fields); i++ {
				tok := strings.Trim(fields[i], "\"'")
				if tok == "--name" && i+1 < len(fields) {
					i++
					name = strings.Trim(fields[i], "\"'")
					continue
				}
				if strings.HasPrefix(tok, "--name=") {
					name = strings.TrimPrefix(tok, "--name=")
					continue
				}
				if strings.HasSuffix(strings.ToLower(tok), ".py") {
					entry = strings.ReplaceAll(tok, "\\", "/")
				}
			}
			if !safeExeName(name) || !zipPathOK(entry) || strings.Contains(entry, ":") {
				continue
			}
			if st, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry))); err == nil && st.Mode().IsRegular() {
				return name, entry
			}
		}
	}
	return "", ""
}
func safeExeName(name string) bool {
	if len(name) == 0 || len(name) > 70 {
		return false
	}
	for _, r := range name {
		if !(r == '_' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func hasTestFiles(root, suffix string) bool {
	found := false
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && strings.HasPrefix(info.Name(), ".") {
			return filepath.SkipDir
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), suffix) {
			found = true
			return io.EOF
		}
		return nil
	})
	return found
}
func exists(s string) bool { _, e := os.Stat(s); return e == nil }
