package cluster

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"storj.io/drpc/drpcmux"
	"storj.io/drpc/drpcserver"

	"github.com/anhostfr/hangar/internal/api/rpc"
	"github.com/anhostfr/hangar/internal/database"
)

type peerSession struct {
	addr   string
	cancel context.CancelFunc
}

type Runtime struct {
	Cluster *Cluster
	Pool    *ConnPool

	listener net.Listener
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	peersMu sync.Mutex
	peers   map[NodeID]*peerSession
	peerWG  sync.WaitGroup
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

	runCtx, cancel := context.WithCancel(ctx)

	pool := NewConnPool(cl.Self(), cl.Secret(), cl.NodeAddr)
	rt := &Runtime{
		Cluster:  cl,
		Pool:     pool,
		listener: ln,
		ctx:      runCtx,
		cancel:   cancel,
		peers:    map[NodeID]*peerSession{},
	}

	srv := rpc.NewServer(cl)
	srv.Metadata = localMetadataAdapter{}
	srv.Chunks = localChunkAdapter{}
	srv.Refs = localRefcountAdapter{}
	srv.Layout = localLayoutAdapter{}
	srv.KV = localKVHandler{}
	srv.Catchup = localCatchupHandler{}
	srv.Joiner = &joinHandler{rt: rt}

	mux := drpcmux.New()
	if err := rpc.DRPCRegisterCluster(mux, srv); err != nil {
		_ = ln.Close()
		cancel()
		return nil, fmt.Errorf("cluster: register rpc: %w", err)
	}

	dsrv := drpcserver.New(mux)

	cl.SetLayoutCallback(rt.reconcilePeers)

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
		_ = cl.RunStalenessLoop(runCtx)
	}()

	SetGlobal(cl)
	SetGlobalRuntime(rt)

	return rt, nil
}

func (r *Runtime) Bootstrap(ctx context.Context, seeds []string) error {
	if err := r.Cluster.LoadLayout(); err != nil {
		return fmt.Errorf("load layout: %w", err)
	}

	if r.Cluster.Layout() != nil {
		r.reconcilePeers()
		return nil
	}

	if len(seeds) == 0 {
		l := &Layout{
			Version: 1,
			Nodes: []LayoutNode{
				r.selfLayoutNode(),
			},
		}
		if err := r.Cluster.ApplyLayout(l); err != nil {
			return fmt.Errorf("auto-apply layout: %w", err)
		}
		return nil
	}

	var lastErr error
	for _, addr := range seeds {
		if err := r.joinViaSeed(ctx, addr); err != nil {
			lastErr = err
			continue
		}
		r.reconcilePeers()
		return nil
	}
	if lastErr == nil {
		lastErr = errors.New("no seeds provided")
	}
	return fmt.Errorf("cluster: failed to join via seeds: %w", lastErr)
}

func (r *Runtime) selfLayoutNode() LayoutNode {
	return LayoutNode{
		ID:       r.Cluster.Self(),
		Addr:     r.Cluster.cfg.Listen,
		Zone:     r.Cluster.cfg.Zone,
		Capacity: r.Cluster.cfg.Capacity,
		Tags:     append([]string(nil), r.Cluster.cfg.Tags...),
	}
}

func (r *Runtime) joinViaSeed(ctx context.Context, seedAddr string) error {
	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, _, err := Dial(dialCtx, seedAddr, string(r.Cluster.Self()), r.Cluster.Secret())
	if err != nil {
		return fmt.Errorf("dial seed %s: %w", seedAddr, err)
	}
	defer conn.Close()

	cli := rpc.NewDRPCClusterClient(conn)
	self := r.selfLayoutNode()
	req := &rpc.JoinRequest{
		Id:       string(self.ID),
		Addr:     self.Addr,
		Zone:     self.Zone,
		Capacity: self.Capacity,
		Tags:     self.Tags,
	}
	resp, err := cli.Join(dialCtx, req)
	if err != nil {
		return fmt.Errorf("join rpc: %w", err)
	}
	if !resp.Accepted {
		return fmt.Errorf("seed refused join: %s", resp.Error)
	}

	l, err := UnmarshalLayout(resp.SignedLayout)
	if err != nil {
		return fmt.Errorf("decode layout: %w", err)
	}
	if err := l.Verify(r.Cluster.Secret()); err != nil {
		return fmt.Errorf("layout signature: %w", err)
	}
	if err := r.Cluster.AdoptLayout(l); err != nil {
		return fmt.Errorf("adopt layout: %w", err)
	}
	return nil
}

func (r *Runtime) reconcilePeers() {
	if r == nil || r.Cluster == nil {
		return
	}

	want := map[NodeID]string{}
	if l := r.Cluster.Layout(); l != nil {
		for _, n := range l.Nodes {
			if n.ID == r.Cluster.Self() {
				continue
			}
			if n.Addr == "" {
				continue
			}
			want[n.ID] = n.Addr
		}
	}

	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	if r.ctx == nil || r.ctx.Err() != nil {
		return
	}

	for id, sess := range r.peers {
		wantAddr, ok := want[id]
		if !ok || wantAddr != sess.addr {
			sess.cancel()
			delete(r.peers, id)
			r.Pool.Drop(id)
		}
	}

	for id, addr := range want {
		if _, ok := r.peers[id]; ok {
			continue
		}
		ctxP, cancel := context.WithCancel(r.ctx)
		r.peers[id] = &peerSession{addr: addr, cancel: cancel}
		r.peerWG.Add(1)
		go func(id NodeID, addr string) {
			defer r.peerWG.Done()
			r.Cluster.PeerLoop(ctxP, id, addr)
		}(id, addr)
	}

	r.Cluster.ReconcileView(want, r.selfLayoutNode())
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

	r.peersMu.Lock()
	for id, sess := range r.peers {
		sess.cancel()
		delete(r.peers, id)
	}
	r.peersMu.Unlock()

	r.peerWG.Wait()
	r.wg.Wait()

	if db := database.LocalStore(); db != nil {
		db.ClearHook()
	}
	if r.Pool != nil {
		r.Pool.Close()
	}
	SetGlobal(nil)
	SetGlobalRuntime(nil)
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
