package cluster

import (
	"context"
	"time"
)

type ClusterMetricsSink func(viewVersion, layoutVersion uint64, alivePeers, totalPeers int, gcLeader bool)

func (c *Cluster) StartMetricsSampler(ctx context.Context, interval time.Duration, sink ClusterMetricsSink) {
	if sink == nil {
		return
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()

	emit := func() {
		view := c.View()
		alive := 0
		for _, ns := range view.Nodes {
			if ns.Status == StatusActive {
				alive++
			}
		}
		sink(view.Version, c.LayoutVersion(), alive, len(view.Nodes), c.IsGCLeader())
	}

	emit()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			emit()
		}
	}
}
