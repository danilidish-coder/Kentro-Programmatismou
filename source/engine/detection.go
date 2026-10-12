package engine

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
