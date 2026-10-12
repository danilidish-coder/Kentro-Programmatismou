package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TrashProject removes one existing project from the active list without
// deleting its source, tests, runs, reports or artifacts.
func (s *Store) TrashProject(id string) (string, error) {
	p, _, err := s.Get(id)
	if err != nil {
		return "", err
	}
	src, err := s.Base(id)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.Root, "trash")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	key := time.Now().UTC().Format("20060102T150405.000000000") + "-" + id + "-" + newID()
	dst := filepath.Join(dir, key)
	if err = os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("Δεν ήταν δυνατή η ασφαλής μεταφορά: %w", err)
	}
	return p.Name, nil
}

// RestoreLatestTrashed moves back the latest removed project, without ever
// overwriting a currently existing project or its data.
func (s *Store) RestoreLatestTrashed() (Project, error) {
	var p Project
	base := filepath.Join(s.Root, "trash")
	entries, err := os.ReadDir(base)
	if err != nil {
		return p, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		src := filepath.Join(base, name)
		b, err := os.ReadFile(filepath.Join(src, "project.json"))
		if err != nil || json.Unmarshal(b, &p) != nil || !validID(p.ID) {
			continue
		}
		if !strings.Contains(name, "-"+p.ID+"-") {
			continue
		}
		dst, err := s.Base(p.ID)
		if err != nil {
			return Project{}, err
		}
		if _, err = os.Stat(dst); err == nil {
			return Project{}, errors.New("Υπάρχει ήδη ενεργό έργο με τον ίδιο κωδικό — δεν αντικαταστάθηκε κανένα αρχείο")
		} else if !errors.Is(err, os.ErrNotExist) {
			return Project{}, err
		}
		if err = os.Rename(src, dst); err != nil {
			return Project{}, err
		}
		return p, nil
	}
	return Project{}, errors.New("Δεν υπάρχει αφαιρεμένο έργο για επαναφορά")
}
