package cluster

import (
	"context"
	"time"

	"storj.io/drpc/drpcconn"

	"github.com/sowahq/hangar/internal/api/rpc"
)

const (
	dialTimeout    = 5 * time.Second
	backoffInitial = time.Second
	backoffMax     = 30 * time.Second
)

func (c *Cluster) RunStalenessLoop(ctx context.Context) error {
	t := time.NewTicker(c.HeartbeatInterval())
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case now := <-t.C:
			c.markStale(now)
		}
	}
}

func (c *Cluster) PeerLoop(ctx context.Context, peerID NodeID, addr string) {
	backoff := backoffInitial

	for {
		if ctx.Err() != nil {
			return
		}

		dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
		conn, _, err := Dial(dialCtx, addr, string(c.cfg.NodeID), c.cfg.Secret, c.cfg.TLSClient)
		cancel()
		if err != nil {
			if !sleepCtx(ctx, backoff) {
				return
			}
			backoff *= 2
			if backoff > backoffMax {
				backoff = backoffMax
			}
			continue
		}
		backoff = backoffInitial

		c.runStream(ctx, conn)
		_ = conn.Close()
	}
}

func (c *Cluster) runStream(ctx context.Context, conn *drpcconn.Conn) {
	cli := rpc.NewDRPCClusterClient(conn)

	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	stream, err := cli.HeartbeatStream(streamCtx)
	if err != nil {
		return
	}
	defer func() { _ = stream.Close() }()

	errCh := make(chan error, 2)

	go func() {
		for {
			hb, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			c.OnHeartbeat(hb)
		}
	}()

	go func() {
		if err := stream.Send(c.BuildHeartbeat()); err != nil {
			errCh <- err
			return
		}

		t := time.NewTicker(c.HeartbeatInterval())
		defer t.Stop()

		for {
			select {
			case <-streamCtx.Done():
				errCh <- streamCtx.Err()
				return
			case <-t.C:
				if err := stream.Send(c.BuildHeartbeat()); err != nil {
					errCh <- err
					return
				}
			}
		}
	}()

	<-errCh
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
