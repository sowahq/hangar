package cluster

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"storj.io/drpc/drpcmux"
	"storj.io/drpc/drpcserver"

	"github.com/anhostfr/hangar/internal/api/rpc"
)

type clusterFixture struct {
	c       *Cluster
	ln      net.Listener
	cancel  context.CancelFunc
	wg      *sync.WaitGroup
	stopped bool
}

func (f *clusterFixture) stop() {
	if f.stopped {
		return
	}
	f.stopped = true
	f.cancel()
	_ = f.ln.Close()
	f.wg.Wait()
}

func pickAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func startNodeOn(t *testing.T, parent context.Context, id NodeID, secret []byte, listenAddr string, peers map[NodeID]string, heartbeatMS int) *clusterFixture {
	t.Helper()

	var ln net.Listener
	var err error
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ln, err = net.Listen("tcp", listenAddr)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("listen %s: %v", listenAddr, err)
	}

	cfg := Config{
		NodeID:      id,
		Listen:      ln.Addr().String(),
		Secret:      secret,
		HeartbeatMS: heartbeatMS,
	}

	c := New(cfg)

	mux := drpcmux.New()
	if err := rpc.DRPCRegisterCluster(mux, rpc.NewServer(c)); err != nil {
		t.Fatalf("register: %v", err)
	}
	srv := drpcserver.New(mux)

	ctx, cancel := context.WithCancel(parent)

	var wg sync.WaitGroup
	wg.Add(2 + len(peers))

	go func() {
		defer wg.Done()
		_ = srv.Serve(ctx, ln)
	}()
	go func() {
		defer wg.Done()
		_ = c.RunStalenessLoop(ctx)
	}()

	for peerID, peerAddr := range peers {
		c.mu.Lock()
		c.view.Upsert(NodeState{ID: peerID, Addr: peerAddr, Status: StatusUnknown})
		c.mu.Unlock()
		go func(pid NodeID, paddr string) {
			defer wg.Done()
			c.PeerLoop(ctx, pid, paddr)
		}(peerID, peerAddr)
	}

	return &clusterFixture{c: c, ln: ln, cancel: cancel, wg: &wg}
}

func waitFor(t *testing.T, timeout time.Duration, msg string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout: %s", msg)
}

func TestHeartbeatTwoNodeConverge(t *testing.T) {
	secret := testSecret(0x42)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrA := pickAddr(t)
	addrB := pickAddr(t)

	a := startNodeOn(t, ctx, "A", secret, addrA, map[NodeID]string{"B": addrB}, 50)
	defer a.stop()

	b := startNodeOn(t, ctx, "B", secret, addrB, map[NodeID]string{"A": addrA}, 50)
	defer b.stop()

	waitFor(t, 3*time.Second, "A sees B active", func() bool {
		return a.c.NodeStatus("B") == StatusActive
	})
	waitFor(t, 3*time.Second, "B sees A active", func() bool {
		return b.c.NodeStatus("A") == StatusActive
	})
}

func TestHeartbeatDownDetection(t *testing.T) {
	secret := testSecret(0x42)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrA := pickAddr(t)
	addrB := pickAddr(t)

	a := startNodeOn(t, ctx, "A", secret, addrA, map[NodeID]string{"B": addrB}, 50)
	defer a.stop()

	b := startNodeOn(t, ctx, "B", secret, addrB, map[NodeID]string{"A": addrA}, 50)

	waitFor(t, 3*time.Second, "A sees B active", func() bool {
		return a.c.NodeStatus("B") == StatusActive
	})

	b.stop()

	waitFor(t, 3*time.Second, "A flips B to down", func() bool {
		return a.c.NodeStatus("B") == StatusDown
	})
}

func TestHeartbeatViewVersionMonotonic(t *testing.T) {
	secret := testSecret(0x42)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addrA := pickAddr(t)
	addrB := pickAddr(t)

	a := startNodeOn(t, ctx, "A", secret, addrA, map[NodeID]string{"B": addrB}, 50)
	defer a.stop()

	b := startNodeOn(t, ctx, "B", secret, addrB, map[NodeID]string{"A": addrA}, 50)
	defer b.stop()

	waitFor(t, 3*time.Second, "A sees B active", func() bool {
		return a.c.NodeStatus("B") == StatusActive
	})

	var prev uint64
	for i := 0; i < 5; i++ {
		v := a.c.ViewVersion()
		if v < prev {
			t.Fatalf("ViewVersion regressed: prev=%d cur=%d", prev, v)
		}
		prev = v
		time.Sleep(60 * time.Millisecond)
	}
}
