package diskspace

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/pkg/sysinfo"
)

var ErrInsufficientStorage = errors.New("insufficient storage")

const usageCacheTTL = 30 * time.Second

var (
	usageMu        sync.Mutex
	usageBytes     int64
	usageMeasured  time.Time
	usageMeasuring bool
)

type Stats struct {
	DataPath     string
	DiskFree     int64
	DiskTotal    int64
	NodeUsed     int64
	MinFreeBytes int64
	MinFreePct   int
	NodeMaxBytes int64
}

func Snapshot() Stats {
	dataPath := config.DataPath()

	return Stats{
		DataPath:     dataPath,
		DiskFree:     sysinfo.DiskFreeBytes(dataPath),
		DiskTotal:    sysinfo.DiskTotalBytes(dataPath),
		NodeUsed:     cachedNodeUsage(dataPath),
		MinFreeBytes: config.MinFreeBytes(),
		MinFreePct:   config.MinFreePct(),
		NodeMaxBytes: config.NodeMaxBytes(),
	}
}

func Check(extra int64) error {
	if extra < 0 {
		extra = 0
	}

	dataPath := config.DataPath()

	minFree := config.MinFreeBytes()
	minPct := config.MinFreePct()
	nodeMax := config.NodeMaxBytes()

	if minFree > 0 || minPct > 0 {
		free := sysinfo.DiskFreeBytes(dataPath)

		if free >= 0 {
			projected := free - extra

			if minFree > 0 && projected < minFree {
				return ErrInsufficientStorage
			}

			if minPct > 0 {
				total := sysinfo.DiskTotalBytes(dataPath)

				if total > 0 {
					threshold := total * int64(minPct) / 100

					if projected < threshold {
						return ErrInsufficientStorage
					}
				}
			}
		}
	}

	if nodeMax > 0 {
		used := cachedNodeUsage(dataPath)

		if used >= 0 && used+extra > nodeMax {
			return ErrInsufficientStorage
		}
	}

	return nil
}

func cachedNodeUsage(dataPath string) int64 {
	usageMu.Lock()

	if !usageMeasured.IsZero() && time.Since(usageMeasured) < usageCacheTTL {
		v := usageBytes
		usageMu.Unlock()
		return v
	}

	if usageMeasuring {
		v := usageBytes
		usageMu.Unlock()
		return v
	}

	usageMeasuring = true
	usageMu.Unlock()

	measured := measureDir(dataPath)

	usageMu.Lock()
	usageBytes = measured
	usageMeasured = time.Now()
	usageMeasuring = false
	usageMu.Unlock()

	return measured
}

func measureDir(root string) int64 {
	var total int64

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		info, statErr := d.Info()
		if statErr != nil {
			return nil
		}

		total += info.Size()
		return nil
	})

	if err != nil {
		return -1
	}

	return total
}

func InvalidateUsageCache() {
	usageMu.Lock()
	usageMeasured = time.Time{}
	usageMu.Unlock()
}
