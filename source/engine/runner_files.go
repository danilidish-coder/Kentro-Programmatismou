package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func exeInventory(root string) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(info.Name()), ".exe") {
			return nil
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		out[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	return out, err
}
func copyFile(src, dst string) error {
	if e := os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
		return e
	}
	a, e := os.Open(src)
	if e != nil {
		return e
	}
	defer a.Close()
	b, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(b, a)
	e2 := b.Close()
	if e != nil {
		return e
	}
	return e2
}

// Build availability is checked before running a time-consuming compiler.
// Missing packages are NOT RUN, not a source-code failure.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		to := filepath.Join(dst, rel)
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Δεν επιτρέπονται συμβολικοί σύνδεσμοι")
		}
		if info.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		if !info.Mode().IsRegular() {
			return errors.New("Μη κανονικό αρχείο")
		}
		if e = os.MkdirAll(filepath.Dir(to), 0700); e != nil {
			return e
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		e2 := out.Close()
		if e != nil {
			return e
		}
		return e2
	})
}
func redact(s string) string {
	for _, key := range []string{"TOKEN", "PASSWORD", "API_KEY", "SECRET"} {
		v := os.Getenv(key)
		if len(v) >= 6 {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	home, _ := os.UserHomeDir()
	if len(home) > 3 {
		s = strings.ReplaceAll(s, home, "[HOME]")
	}
	return s
}
