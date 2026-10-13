//go:build !windows

package web

import (
	"os"
	"path/filepath"
)

type DriveInfo struct {
	Letter    string `json:"letter"`
	Path      string `json:"path"`
	Removable bool   `json:"removable"`
	Ready     bool   `json:"ready"`
}

func getAvailableDrives() []DriveInfo {
	var list []DriveInfo
	entries, err := os.ReadDir("/Volumes")
	if err == nil {
		for _, e := range entries {
			list = append(list, DriveInfo{
				Letter:    e.Name(),
				Path:      filepath.Join("/Volumes", e.Name()),
				Removable: true,
				Ready:     true,
			})
		}
	}
	return list
}
