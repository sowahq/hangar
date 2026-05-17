package cluster

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"storj.io/drpc/drpcmux"
	"storj.io/drpc/drpcserver"

	"github.com/anhostfr/hangar/internal/api/rpc"
)

type Runtime struct {
	Cluster *Cluster

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

	mux := drpcmux.New()
	if err := rpc.DRPCRegisterCluster(mux, rpc.NewServer(cl)); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("cluster: register rpc: %w", err)
	}

	srv := drpcserver.New(mux)

	runCtx, cancel := context.WithCancel(ctx)

	rt := &Runtime{Cluster: cl, listener: ln, cancel: cancel}

	rt.wg.Add(2)

	go func() {
		defer rt.wg.Done()
		_ = srv.Serve(runCtx, ln)
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
	SetGlobal(nil)
}

func (r *Runtime) Addr() string {
	if r == nil || r.listener == nil {
		return ""
	}
	return r.listener.Addr().String()
}
