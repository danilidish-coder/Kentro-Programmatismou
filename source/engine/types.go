package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ProductVersion = "1.0.0-beta2"

type Step struct {
	Name           string   `json:"name"`
	ExpectContains string   `json:"expect_contains,omitempty"`
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Required       *bool    `json:"required,omitempty"`
}

func (s Step) Mandatory() bool { return s.Required == nil || *s.Required }

type StressSpec struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"` // parallel or restart
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	Parallel       int      `json:"parallel"`
	Repeats        int      `json:"repeats"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Health         *Step    `json:"health,omitempty"`
	Required       *bool    `json:"required,omitempty"`
}

func (s StressSpec) Mandatory() bool { return s.Required == nil || *s.Required }

type Manifest struct {
	Name     string       `json:"name"`
	Version  string       `json:"version"`
	Language string       `json:"language,omitempty"`
	Entry    string       `json:"entry,omitempty"`
	WorkDir  string       `json:"workdir,omitempty"`
	Tests    []Step       `json:"tests"`
	Stress   []StressSpec `json:"stress"`
	Build    *Step        `json:"build"`
	Smoke    *Step        `json:"smoke"`
}

type Project struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Version    string    `json:"version"`
	Language   string    `json:"language"`
	SourceHash string    `json:"source_hash"`
	ImportedAt time.Time `json:"imported_at"`
}

type Result struct {
	Name       string `json:"name"`
	Phase      string `json:"phase"`
	Status     string `json:"status"`
	Required   bool   `json:"required"`
	Reason     string `json:"reason,omitempty"`
	Command    string `json:"command,omitempty"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	LogFile    string `json:"log_file,omitempty"`
}

type Artifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Run struct {
	ID         string     `json:"id"`
	ProjectID  string     `json:"project_id"`
	SourceHash string     `json:"source_hash"`
	PlanHash   string     `json:"plan_hash"`
	HostOS     string     `json:"host_os"`
	Started    time.Time  `json:"started"`
	Finished   time.Time  `json:"finished"`
	Results    []Result   `json:"results"`
	Artifacts  []Artifact `json:"artifacts,omitempty"`
	Blocked    bool       `json:"blocked"`
}

type Store struct{ Root string }

func DefaultRoot() string {
	if x := os.Getenv("LOCALAPPDATA"); x != "" {
		return filepath.Join(x, "KentroProgrammatismou_Clean")
	}
	d, e := os.UserConfigDir()
	if e != nil {
		return ".kentrop"
	}
	return filepath.Join(d, "KentroProgrammatismou_Clean")
}
func NewStore(root string) (*Store, error) {
	if root == "" {
		root = DefaultRoot()
	}
	for _, dir := range []string{"projects", "reports", "backups"} {
		if e := os.MkdirAll(filepath.Join(root, dir), 0700); e != nil {
			return nil, e
		}
	}
	return &Store{Root: root}, nil
}
func (s *Store) Base(id string) (string, error) {
	if !validID(id) {
		return "", errors.New("Μη έγκυρος κωδικός έργου")
	}
	return filepath.Join(s.Root, "projects", id), nil
}
func validID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return false
		}
	}
	return true
}
func atomicJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'))
}
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".temp-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return atomicReplace(name, path)
}
func (s *Store) List() ([]Project, error) {
	dirs, e := os.ReadDir(filepath.Join(s.Root, "projects"))
	if e != nil {
		return nil, e
	}
	out := []Project{}
	for _, d := range dirs {
		if !d.IsDir() || !validID(d.Name()) {
			continue
		}
		b, e := os.ReadFile(filepath.Join(s.Root, "projects", d.Name(), "project.json"))
		if e != nil {
			continue
		}
		var p Project
		if json.Unmarshal(b, &p) == nil && p.ID == d.Name() {
			out = append(out, p)
		}
	}
	return out, nil
}
func (s *Store) Get(id string) (Project, Manifest, error) {
	var p Project
	var m Manifest
	base, e := s.Base(id)
	if e != nil {
		return p, m, e
	}
	b, e := os.ReadFile(filepath.Join(base, "project.json"))
	if e != nil {
		return p, m, e
	}
	if e = json.Unmarshal(b, &p); e != nil {
		return p, m, e
	}
	if p.ID != id {
		return p, m, errors.New("Ασυμφωνία κωδικού έργου")
	}
	b, e = os.ReadFile(filepath.Join(base, "source", "project-manifest.json"))
	if errors.Is(e, os.ErrNotExist) {
		m = AutoManifest(filepath.Join(base, "source"))
		return p, m, nil
	}
	if e != nil {
		return p, m, e
	}
	e = json.Unmarshal(b, &m)
	if e != nil {
		return p, m, fmt.Errorf("Μη έγκυρο σχέδιο ελέγχων: %w", e)
	}
	return p, m, ValidateManifest(m)
}
func ValidateManifest(m Manifest) error {
	if m.WorkDir != "" && m.WorkDir != "." && !zipPathOK(m.WorkDir) {
		return errors.New("Μη ασφαλής διαδρομή workdir")
	}
	if len(m.Name) > 150 || len(m.Version) > 100 {
		return errors.New("Υπερβολικό μήκος στοιχείων έργου")
	}
	if len(m.Tests) > 100 || len(m.Stress) > 30 {
		return errors.New("Πάρα πολλά σενάρια")
	}
	for _, t := range m.Tests {
		if e := validateStep(t); e != nil {
			return e
		}
	}
	if m.Build != nil {
		if e := validateStep(*m.Build); e != nil {
			return e
		}
	}
	if m.Smoke != nil {
		if e := validateStep(*m.Smoke); e != nil {
			return e
		}
	}
	for _, x := range m.Stress {
		if x.Parallel < 0 || x.Parallel > 16 || x.Repeats < 0 || x.Repeats > 50 || x.TimeoutSeconds > 120 {
			return errors.New("Μη έγκυρες παράμετροι πίεσης")
		}
		if x.Kind != "parallel" && x.Kind != "restart" {
			return errors.New("Μη αναγνωρισμένο είδος δοκιμής πίεσης")
		}
		if e := validateStep(Step{Command: x.Command, Args: x.Args, Name: x.Name, TimeoutSeconds: x.TimeoutSeconds}); e != nil {
			return e
		}
		if x.Health != nil {
			if e := validateStep(*x.Health); e != nil {
				return e
			}
		}
	}
	return nil
}
func validateStep(s Step) error {
	if strings.TrimSpace(s.Command) == "" || len(s.Args) > 64 || len(s.Name) > 200 || s.TimeoutSeconds < 0 || s.TimeoutSeconds > 1800 {
		return errors.New("Μη έγκυρη εντολή/διάρκεια ελέγχου")
	}
	for _, v := range append([]string{s.Command}, s.Args...) {
		if len(v) > 4000 || strings.ContainsRune(v, 0) {
			return errors.New("Μη έγκυρη παράμετρος εντολής")
		}
	}
	return nil
}
