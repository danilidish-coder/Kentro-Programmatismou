package engine

import (
	"archive/zip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
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
