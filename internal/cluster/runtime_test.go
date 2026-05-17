package cluster

import (
	"context"
	"net"
	"testing"
	"time"
)

func freeAddrRuntime(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func TestStartAndStop(t *testing.T) {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i)
	}

	addr := freeAddrRuntime(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rt, err := Start(ctx, Config{
		NodeID:      "n1",
		Listen:      addr,
		Secret:      secret,
		HeartbeatMS: 50,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	if rt.Addr() == "" {
		t.Fatalf("expected addr")
	}
	if Global() == nil {
		t.Fatalf("expected global cluster set")
	}
	if Global().Self() != "n1" {
		t.Fatalf("self=%q", Global().Self())
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("tcp", rt.Addr())
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	rt.Stop()

	if Global() != nil {
		t.Fatalf("expected global nil after stop")
	}
}
