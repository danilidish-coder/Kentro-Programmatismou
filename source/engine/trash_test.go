package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrashAndRestoreDoesNotDeleteData(t *testing.T) {
	s := newTestStore(t)
	p, e := s.ImportZIP(zipFixture(t, map[string]string{"src/example.py": "print('sample')\n", "README.md": "unique content"}))
	if e != nil {
		t.Fatal(e)
	}
	base, _ := s.Base(p.ID)
	secret := filepath.Join(base, "keep-this-test-proof.txt")
	if e = os.WriteFile(secret, []byte("immutable proof"), 0600); e != nil {
		t.Fatal(e)
	}
	name, e := s.TrashProject(p.ID)
	if e != nil || name != p.Name {
		t.Fatalf("trash %q %v", name, e)
	}
	ps, e := s.List()
	if e != nil || len(ps) != 0 {
		t.Fatalf("expected empty active list: %v %v", ps, e)
	}
	trash, e := os.ReadDir(filepath.Join(s.Root, "trash"))
	if e != nil || len(trash) != 1 {
		t.Fatalf("expected reversible trash: %v %v", trash, e)
	}
	p2, e := s.RestoreLatestTrashed()
	if e != nil || p2.ID != p.ID {
		t.Fatalf("restore project: %+v %v", p2, e)
	}
	b, e := os.ReadFile(secret)
	if e != nil || string(b) != "immutable proof" {
		t.Fatalf("lost original data: %s %v", string(b), e)
	}
	ps, e = s.List()
	if e != nil || len(ps) != 1 {
		t.Fatalf("expected restored list: %v %v", ps, e)
	}
	if _, e = s.RestoreLatestTrashed(); e == nil {
		t.Fatal("unexpected duplicate restoration")
	}
}

func TestTrashRestoreRefusesOverwriteExistingData(t *testing.T) {
	s := newTestStore(t)
	p, err := s.ImportZIP(zipFixture(t, map[string]string{"main.py": "print('original')"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.TrashProject(p.ID); err != nil {
		t.Fatal(err)
	}
	base, _ := s.Base(p.ID)
	if err = os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	proof := filepath.Join(base, "new-unique-file.txt")
	if err = os.WriteFile(proof, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RestoreLatestTrashed(); err == nil {
		t.Fatal("overwrote active project")
	}
	b, err := os.ReadFile(proof)
	if err != nil || string(b) != "untouched" {
		t.Fatalf("existing data altered %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(s.Root, "trash"))
	if len(entries) != 1 {
		t.Fatal("lost trashed copy after conflicting restore")
	}
}

func TestTrashRejectsUnsafeIDs(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"../../etc/passwd", ".", "foo\\..\\secret", ""} {
		if _, err := s.TrashProject(id); err == nil {
			t.Fatalf("unsafe ID accepted: %q", id)
		}
	}
}
