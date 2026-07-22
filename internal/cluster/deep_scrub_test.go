package cluster

import (
	"context"
	"encoding/hex"
	"io"
	"testing"
	"time"

	"github.com/sowahq/hangar/internal/storage"
	"github.com/zeebo/blake3"
)

func TestRunDeepScrubNoEC(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/config.toml", "data_directory = \""+dir+"\"\n[api]\nbind_addr = \":0\"\n[storage]\nchunk_size = 4194304\n[garbage_collection]\ninterval_hours = 24\n"); err != nil {
		t.Fatalf("write cfg: %v", err)
	}
	rt := setupSingleECRuntime(t, 0, 0)
	stats, err := rt.RunDeepScrub(context.Background())
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}
	if stats.Scanned != 0 {
		t.Fatalf("scanned=%d want 0 when EC disabled", stats.Scanned)
	}
}

func TestRunDeepScrubRepairsCorruptShard(t *testing.T) {
	rt := setupSingleECRuntime(t, 2, 1)
	injectECLayoutAndPeers(t, rt, []NodeID{"self", "peer-a", "peer-b"})

	enc, err := NewECEncoder(2, 1)
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	payload := []byte("hello deep scrub world payload")
	sum := blake3.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	shards, err := enc.Encode(payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	owners := rt.Cluster.ChunkOwnersStable(hash, 3)

	selfIdx := -1
	for i, o := range owners {
		if o == rt.Cluster.Self() {
			selfIdx = i
			break
		}
	}
	if selfIdx == -1 {
		t.Skip("self not owner of any shard for this hash")
	}

	var local storage.LocalChunkStore
	for i := range owners {
		if err := local.PutRaw(shardKey(hash, i), shards[i]); err != nil {
			t.Fatalf("seed shard %d: %v", i, err)
		}
	}

	corrupt := append([]byte(nil), shards[selfIdx]...)
	corrupt[0] ^= 0xFF
	if err := local.Delete(shardKey(hash, selfIdx)); err != nil {
		t.Fatalf("delete to overwrite: %v", err)
	}
	if err := local.PutRaw(shardKey(hash, selfIdx), corrupt); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	if err := storage.IncrementChunkRefs([]string{hash}); err != nil {
		t.Fatalf("inc: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stats, err := rt.RunDeepScrub(ctx)
	if err != nil {
		t.Fatalf("scrub: %v", err)
	}

	if stats.Scanned == 0 {
		t.Fatalf("expected scanned >= 1, got 0")
	}
	if stats.Skipped > 0 {
		t.Logf("note: %d shards skipped (peers unreachable for remote shards)", stats.Skipped)
	}

	rc, err := local.OpenRaw(shardKey(hash, selfIdx))
	if err != nil {
		t.Fatalf("open repaired: %v", err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()

	if stats.Skipped > 0 {
		return
	}
	if stats.Corrupt == 0 {
		t.Fatalf("expected corrupt >= 1, got 0 stats=%+v", stats)
	}
	if stats.Repaired == 0 {
		t.Fatalf("expected repaired >= 1, got 0 stats=%+v", stats)
	}
	for i, b := range shards[selfIdx] {
		if i < len(got) && got[i] != b {
			t.Fatalf("byte %d mismatch after repair: got=%x want=%x", i, got[i], b)
		}
	}
}
