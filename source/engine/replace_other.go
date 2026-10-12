//go:build !windows

package engine

import "os"

func atomicReplace(source, destination string) error { return os.Rename(source, destination) }
