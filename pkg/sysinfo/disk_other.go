//go:build !unix && !windows

package sysinfo

func DiskFreeBytes(path string) int64 {
	return -1
}

func DiskTotalBytes(path string) int64 {
	return -1
}
