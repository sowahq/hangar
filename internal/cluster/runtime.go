package cluster

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"storj.io/drpc/drpcmux"
	"storj.io/drpc/drpcserver"

	"github.com/anhostfr/hangar/internal/api/rpc"
	"github.com/anhostfr/hangar/internal/database"
)

type Runtime struct {
	Cluster *Cluster
	Pool    *ConnPool

	listener net.Listener
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func ParsePeers(entries []string) (map[NodeID]string, error) {
	out := map[NodeID]string{}

	for _, raw := range entries {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		sep := strings.IndexAny(entry, "@=")
		if sep <= 0 || sep == len(entry)-1 {
			return nil, fmt.Errorf("cluster: peer %q must be in form id@host:port", raw)
		}

		id := NodeID(strings.TrimSpace(entry[:sep]))
		addr := strings.TrimSpace(entry[sep+1:])

		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("cluster: duplicate peer id %q", id)
		}

		out[id] = addr
	}

	return out, nil
}

func Start(ctx context.Context, cfg Config) (*Runtime, error) {
	if cfg.NodeID == "" {
		return nil, fmt.Errorf("cluster: node_id required")
	}
	if cfg.Listen == "" {
		return nil, fmt.Errorf("cluster: listen required")
	}

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return nil, fmt.Errorf("cluster: listen %s: %w", cfg.Listen, err)
	}

	cl := New(cfg)

	srv := rpc.NewServer(cl)
	srv.Metadata = localMetadataAdapter{}
	srv.Chunks = localChunkAdapter{}
	srv.Refs = localRefcountAdapter{}
	srv.Layout = localLayoutAdapter{}
	srv.KV = localKVHandler{}
	srv.Catchup = localCatchupHandler{}

	mux := drpcmux.New()
	if err := rpc.DRPCRegisterCluster(mux, srv); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("cluster: register rpc: %w", err)
	}

	dsrv := drpcserver.New(mux)

	runCtx, cancel := context.WithCancel(ctx)

	pool := NewConnPool(cl.Self(), cl.Secret(), cl.NodeAddr)
	rt := &Runtime{Cluster: cl, Pool: pool, listener: ln, cancel: cancel}

	if db := database.LocalStore(); db != nil {
		db.SetHook(newKVReplicator(cl, pool))
	}

	rt.wg.Add(2)

	go func() {
		defer rt.wg.Done()
		_ = dsrv.Serve(runCtx, ln)
	}()

	go func() {
		defer rt.wg.Done()
		_ = cl.RunHeartbeat(runCtx)
	}()

	SetGlobal(cl)

	return rt, nil
}

func (r *Runtime) Stop() {
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	if r.listener != nil {
		_ = r.listener.Close()
	}
	r.wg.Wait()
	if db := database.LocalStore(); db != nil {
		db.ClearHook()
	}
	if r.Pool != nil {
		r.Pool.Close()
	}
	SetGlobal(nil)
}

func (r *Runtime) BootstrapPeerSync(ctx context.Context, attempts int, delay time.Duration) (int, error) {
	if r == nil || r.Cluster == nil || r.Pool == nil {
		return 0, nil
	}
	for i := 0; i < attempts; i++ {
		view := r.Cluster.View()
		for id, ns := range view.Nodes {
			if id == r.Cluster.Self() || ns.Status != StatusActive {
				continue
			}
			count, err := PullBulkSyncFrom(ctx, r.Pool, id)
			if err == nil {
				return count, nil
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(delay):
		}
	}
	return 0, nil
}

func (r *Runtime) Addr() string {
	if r == nil || r.listener == nil {
		return ""
	}
	return r.listener.Addr().String()
}
