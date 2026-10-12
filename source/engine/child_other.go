//go:build !windows

package engine

import "os/exec"

func prepareChild(cmd *exec.Cmd) {}
