package cluster

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/anhostfr/hangar/internal/api/rpc"
	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/storage"
)

func setupCatchupNode(t *testing.T, id NodeID, listen string, peers map[NodeID]string) (*Runtime, func()) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := writeFile(cfgPath, "data_directory = \""+dir+"\"\n[api]\nbind_addr = \":0\"\n[storage]\nchunk_size = 4194304\n[garbage_collection]\ninterval_hours = 24\n"); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	if err := config.LoadServerConfig(cfgPath); err != nil {
		t.Fatalf("load cfg: %v", err)
	}

	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	_ = peers
	rt, err := Start(ctx, Config{
		NodeID:      id,
		Listen:      listen,
		Secret:      secret,
		HeartbeatMS: 100,
	})
	if err != nil {
		cancel()
		t.Fatalf("start: %v", err)
	}

	cleanup := func() {
		rt.Stop()
		cancel()
		_ = database.Close()
	}
	return rt, cleanup
}

func TestReplicaCatchupStreamsWALEntries(t *testing.T) {
	addr := freeAddrRuntime(t)

	rt, cleanup := setupCatchupNode(t, "primary", addr, nil)
	defer cleanup()

	var local storage.LocalMetadataStore
	if err := local.PutRaw("b", "k1", []byte("v1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := AppendWAL("put", "b", "k1", []byte("v1")); err != nil {
		t.Fatalf("wal: %v", err)
	}
	if err := AppendWAL("put", "b", "k2", []byte("v2")); err != nil {
		t.Fatalf("wal: %v", err)
	}
	if err := AppendWAL("del", "b", "k1", nil); err != nil {
		t.Fatalf("wal: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := Dial(ctx, rt.Addr(), "primary", rt.Cluster.Secret(), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	cli := rpc.NewDRPCClusterClient(conn)

	stream, err := cli.ReplicaCatchup(ctx, &rpc.CatchupCursor{LastSeq: 0})
	if err != nil {
		t.Fatalf("catchup: %v", err)
	}

	var got []string
	for {
		e, rerr := stream.Recv()
		if rerr != nil {
			break
		}
		got = append(got, e.OpType)
	}

	if len(got) != 3 {
		t.Fatalf("got %d wal entries: %v", len(got), got)
	}
	want := []string{"put", "put", "del"}
	for i, op := range want {
		if got[i] != op {
			t.Fatalf("entry %d op=%q want %q", i, got[i], op)
		}
	}
}

