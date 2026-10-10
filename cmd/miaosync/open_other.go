//go:build !windows

package main

import (
	"os/exec"
	"runtime"
)

// openURL 非 Windows 平台用系统命令打开浏览器。
func openURL(url string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}

// fatalBox 非 Windows 平台错误走 stderr（有控制台可见），无需消息框。
func fatalBox(_, _ string) {}
