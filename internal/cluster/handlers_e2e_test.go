package cluster

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/anhostfr/hangar/internal/api/rpc"
	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
)

func setupHandlerNode(t *testing.T, listen string, peers map[NodeID]string, id NodeID) (*Runtime, func()) {
	t.Helper()
	dir := t.TempDir()

	cfgPath := filepath.Join(dir, "config.toml")
	contents := "data_directory = \"" + dir + "\"\n[api]\nbind_addr = \":0\"\n[storage]\nchunk_size = 4194304\n[garbage_collection]\ninterval_hours = 24\n"
	if err := writeFileT(t, cfgPath, contents); err != nil {
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
	rt, err := Start(ctx, Config{
		NodeID:      id,
		Listen:      listen,
		Peers:       peers,
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

func writeFileT(t *testing.T, path, contents string) error {
	t.Helper()
	return writeFile(path, contents)
}

func TestMetadataRPCRoundtrip(t *testing.T) {
	addr := freeAddrRuntime(t)

	rt, cleanup := setupHandlerNode(t, addr, nil, "a")
	defer cleanup()

	secret := rt.Cluster.Secret()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := Dial(ctx, rt.Addr(), "a", secret)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cli := rpc.NewDRPCClusterClient(conn)

	ack, err := cli.PutMetadata(ctx, &rpc.MetadataOp{Bucket: "b", Key: "obj1", Metadata: []byte(`{"key":"obj1"}`)})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if !ack.Ok {
		t.Fatalf("put nok: %s", ack.Error)
	}

	got, err := cli.GetMetadata(ctx, &rpc.MetadataKey{Bucket: "b", Key: "obj1"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !got.Found || string(got.Metadata) != `{"key":"obj1"}` {
		t.Fatalf("found=%v meta=%q", got.Found, got.Metadata)
	}

	miss, err := cli.GetMetadata(ctx, &rpc.MetadataKey{Bucket: "b", Key: "missing"})
	if err != nil {
		t.Fatalf("get miss: %v", err)
	}
	if miss.Found {
		t.Fatalf("expected not found")
	}

	del, err := cli.DeleteMetadata(ctx, &rpc.MetadataKey{Bucket: "b", Key: "obj1"})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !del.Found {
		t.Fatalf("delete not found")
	}
}

func TestChunkRPCRoundtrip(t *testing.T) {
	addr := freeAddrRuntime(t)
	rt, cleanup := setupHandlerNode(t, addr, nil, "a")
	defer cleanup()

	secret := rt.Cluster.Secret()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := Dial(ctx, rt.Addr(), "a", secret)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cli := rpc.NewDRPCClusterClient(conn)

	hash := "0000000000000000000000000000000000000000000000000000000000abcdef"
	payload := []byte("hello world")

	stream, err := cli.PutChunk(ctx)
	if err != nil {
		t.Fatalf("put stream: %v", err)
	}
	if err := stream.Send(&rpc.ChunkData{Hash: hash, Payload: payload, Last: true}); err != nil {
		t.Fatalf("send: %v", err)
	}
	ack, err := stream.CloseAndRecv()
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if !ack.Stored {
		t.Fatalf("not stored: %s", ack.Error)
	}

	pres, err := cli.HasChunk(ctx, &rpc.ChunkRef{Hash: hash})
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	if !pres.Present {
		t.Fatalf("not present")
	}

	getStream, err := cli.GetChunk(ctx, &rpc.ChunkRef{Hash: hash})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var got []byte
	for {
		msg, err := getStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		got = append(got, msg.Payload...)
		if msg.Last {
			break
		}
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q want %q", got, payload)
	}

	delAck, err := cli.DeleteChunkReplica(ctx, &rpc.ChunkRef{Hash: hash})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !delAck.Ok {
		t.Fatalf("delete nok: %s", delAck.Error)
	}
}

func TestRefRPC(t *testing.T) {
	addr := freeAddrRuntime(t)
	rt, cleanup := setupHandlerNode(t, addr, nil, "a")
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := Dial(ctx, rt.Addr(), "a", rt.Cluster.Secret())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	cli := rpc.NewDRPCClusterClient(conn)

	ack, err := cli.IncRefs(ctx, &rpc.RefDelta{Hashes: []string{"h1", "h2"}})
	if err != nil || !ack.Ok {
		t.Fatalf("inc err=%v ack=%+v", err, ack)
	}
	ack, err = cli.DecRefs(ctx, &rpc.RefDelta{Hashes: []string{"h1"}})
	if err != nil || !ack.Ok {
		t.Fatalf("dec err=%v ack=%+v", err, ack)
	}
}
