//go:build windows

package sysinfo

import "golang.org/x/sys/windows"

func DiskFreeBytes(path string) int64 {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return -1
	}
	var freeAvail, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &freeAvail, &total, &totalFree); err != nil {
		return -1
	}
	return int64(freeAvail)
}
