//go:build windows

package web

import (
	"syscall"
	"unsafe"
)

type DriveInfo struct {
	Letter    string `json:"letter"`    // "C:", "D:", "E:"
	Path      string `json:"path"`      // "C:\", "D:\", "E:\"
	Removable bool   `json:"removable"` // 是否为可移动驱动器（U盘 / 移动硬盘）
	Ready     bool   `json:"ready"`
}

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procGetDriveType = kernel32.NewProc("GetDriveTypeW")
)

const (
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDROM     = 5
	driveRAMDisk   = 6
)

func getAvailableDrives() []DriveInfo {
	var list []DriveInfo
	for c := 'C'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		ptr, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		r, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(ptr)))
		dt := uint32(r)
		if dt == driveFixed || dt == driveRemovable {
			list = append(list, DriveInfo{
				Letter:    string(c) + ":",
				Path:      root,
				Removable: (dt == driveRemovable),
				Ready:     true,
			})
		}
	}
	return list
}
