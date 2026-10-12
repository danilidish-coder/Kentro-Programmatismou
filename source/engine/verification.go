package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

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
