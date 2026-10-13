//go:build !windows

package web

import "os/exec"

func setCmdNoWindow(cmd *exec.Cmd) {}
