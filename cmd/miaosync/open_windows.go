//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	shell32DLL        = syscall.NewLazyDLL("shell32.dll")
	user32DLL         = syscall.NewLazyDLL("user32.dll")
	procShellExecuteW = shell32DLL.NewProc("ShellExecuteW")
	procMessageBoxW   = user32DLL.NewProc("MessageBoxW")
)

// openURL 通过 ShellExecuteW 调用系统默认浏览器打开 URL。
// 相比 cmd /c start：在 GUI 子系统（-H windowsgui）下无控制台依赖、行为确定。
func openURL(url string) error {
	u, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	// ShellExecuteW(hwnd, op, file, params, dir, show)：op=NULL → 默认动词 open
	r, _, callErr := procShellExecuteW.Call(
		0,
		0,
		uintptr(unsafe.Pointer(u)),
		0,
		0,
		1, // SW_SHOWNORMAL
	)
	if r <= 32 { // ShellExecute 返回值 ≤32 均为失败
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return syscall.EINVAL
	}
	return nil
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
