package metrics

import (
	"context"
	"time"

	"github.com/anhostfr/hangar/internal/service/diskspace"
)

const diskSampleInterval = 30 * time.Second

func StartDiskSampler(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	sample := func() {
		s := diskspace.Snapshot()
		ObserveDisk(s.DiskFree, s.DiskTotal, s.NodeUsed, s.NodeMaxBytes)
	}

	sample()

	ticker := time.NewTicker(diskSampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sample()
		}
	}
}
