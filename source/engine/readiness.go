package engine

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Readiness is non-invasive: it does not install tools or download packages.
// It is intended for the non-programmer's step 2 of the graphical wizard.
func Readiness(m Manifest) []string {
	required := map[string]bool{}
	for _, x := range m.Tests {
		required[strings.ToLower(x.Command)] = true
	}
	for _, x := range m.Stress {
		required[strings.ToLower(x.Command)] = true
	}
	if m.Build != nil {
		required[strings.ToLower(m.Build.Command)] = true
	}
	var results []string
	label := map[string]string{"go": "Go", "python": "Python", "python3": "Python", "py": "Python", "pyinstaller": "PyInstaller", "dotnet": ".NET", "cargo": "Rust", "node": "Node.js", "npm": "Node.js", "pytest": "Python tests"}
	commands := []string{"python", "python3", "py", "go", "dotnet", "cargo", "node", "npm", "pytest", "pyinstaller"}
	for _, command := range commands {
		if !required[command] {
			continue
		}
		if _, err := exec.LookPath(command); err != nil {
			results = append(results, fmt.Sprintf("%s: λείπει από τον υπολογιστή — οι αντίστοιχοι έλεγχοι δεν θα εκτελεστούν", label[command]))
		} else {
			results = append(results, label[command]+": διαθέσιμο")
		}
	}
	if m.Build != nil && strings.EqualFold(m.Build.Command, "python") && len(m.Build.Args) > 1 && m.Build.Args[0] == "-m" && m.Build.Args[1] == "PyInstaller" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		python, err := exec.LookPath("python")
		if err == nil {
			cmd := exec.CommandContext(ctx, python, "-c", "import importlib.util,sys;sys.exit(0 if importlib.util.find_spec('PyInstaller') else 1)")
			cmd.Env = offlineEnv()
			if cmd.Run() == nil {
				results = append(results, "Κατασκευή EXE Python: διαθέσιμη")
			} else {
				results = append(results, "Κατασκευή EXE Python: λείπει το PyInstaller — δεν θα γίνει αυτόματη εγκατάσταση")
			}
		}
	}
	if len(results) == 0 {
		results = append(results, "Δεν αναγνωρίστηκε απαιτούμενο εργαλείο. Ενδέχεται να χρειάζεται συγκεκριμένο σχέδιο δοκιμών.")
	}
	return results
}
