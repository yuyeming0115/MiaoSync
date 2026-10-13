//go:build windows

package main

import (
	"os/exec"
	"syscall"
	"unsafe"
)

var (
	shell32DLL        = syscall.NewLazyDLL("shell32.dll")
	user32DLL         = syscall.NewLazyDLL("user32.dll")
	procShellExecuteW = shell32DLL.NewProc("ShellExecuteW")
	procMessageBoxW   = user32DLL.NewProc("MessageBoxW")
)

// openURL 调用系统默认浏览器打开 URL。
// 使用 rundll32 url.dll,FileProtocolHandler 避免 Go 运行时非 STA 线程直接调用 ShellExecuteW 引发的异常崩溃。
func openURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	return cmd.Start()
}

// fatalBox 弹出原生错误消息框。windowsgui 构建没有控制台，
// 静默退出会让用户以为"双击没反应"，任何致命错误都必须可见。
func fatalBox(title, msg string) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(msg)
	procMessageBoxW.Call(
		0,
		uintptr(unsafe.Pointer(m)),
		uintptr(unsafe.Pointer(t)),
		0x00000010, // MB_ICONERROR | MB_OK
	)
}
