//go:build windows

package web

import (
	"os/exec"
	"syscall"
)

func setCmdNoWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW: 隐藏任何控制台黑窗
	}
}
