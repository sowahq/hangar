package cluster

import (
	"context"
	"net"
	"testing"
	"time"
)

func TestParsePeers(t *testing.T) {
	cases := []struct {
		name    string
		in      []string
		want    map[NodeID]string
		wantErr bool
	}{
		{name: "empty"},
		{name: "single at", in: []string{"n2@10.0.0.2:7000"}, want: map[NodeID]string{"n2": "10.0.0.2:7000"}},
		{name: "single eq", in: []string{"n2=10.0.0.2:7000"}, want: map[NodeID]string{"n2": "10.0.0.2:7000"}},
		{name: "multi", in: []string{"a@1.1.1.1:7", "b@2.2.2.2:7"}, want: map[NodeID]string{"a": "1.1.1.1:7", "b": "2.2.2.2:7"}},
		{name: "whitespace", in: []string{"  a @ 1.1.1.1:7  ", ""}, want: map[NodeID]string{"a": "1.1.1.1:7"}},
		{name: "missing sep", in: []string{"abc"}, wantErr: true},
		{name: "empty id", in: []string{"@1.1.1.1:7"}, wantErr: true},
		{name: "empty addr", in: []string{"a@"}, wantErr: true},
		{name: "dup id", in: []string{"a@1:1", "a@2:2"}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePeers(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d want=%d", len(got), len(tc.want))
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("%q: got %q want %q", k, got[k], v)
				}
			}
		})
	}
}

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
