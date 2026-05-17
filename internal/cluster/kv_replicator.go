package cluster

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/anhostfr/hangar/internal/api/rpc"
	"github.com/anhostfr/hangar/internal/database"
)

var ReplicatedPrefixes = [][]byte{
	[]byte("s3key:"),
	[]byte("bucket:"),
	[]byte("token:"),
	[]byte("encryption:"),
	[]byte("objectlock:"),
	[]byte("ssekr:"),
	[]byte("lifecycle:"),
	[]byte("cors:"),
	[]byte("tagging:"),
	[]byte("website:"),
	[]byte("logging:"),
	[]byte("cluster:layout:"),
	[]byte("mpu:"),
	[]byte("mpupart:"),
	[]byte("version:"),
}

func isReplicated(key []byte) bool {
	for _, p := range ReplicatedPrefixes {
		if bytes.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

type kvReplicator struct {
	cl   *Cluster
	pool *ConnPool
}

func newKVReplicator(cl *Cluster, pool *ConnPool) *kvReplicator {
	return &kvReplicator{cl: cl, pool: pool}
}

func (r *kvReplicator) OnPut(key, value []byte) {
	if !isReplicated(key) {
		return
	}
	keyCopy := append([]byte(nil), key...)
	valCopy := append([]byte(nil), value...)
	go r.fanout(&rpc.KVOp{Op: "put", Key: keyCopy, Value: valCopy})
}

func (r *kvReplicator) OnDelete(key []byte) {
	if !isReplicated(key) {
		return
	}
	keyCopy := append([]byte(nil), key...)
	go r.fanout(&rpc.KVOp{Op: "del", Key: keyCopy})
}

func (r *kvReplicator) fanout(op *rpc.KVOp) {
	view := r.cl.View()
	var wg sync.WaitGroup
	for id, ns := range view.Nodes {
		if id == r.cl.Self() || ns.Status != StatusActive {
			continue
		}
		wg.Add(1)
		go func(id NodeID) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cli, err := r.pool.Client(ctx, id)
			if err != nil {
				return
			}
			_, _ = cli.ReplicateKV(ctx, op)
		}(id)
	}
	wg.Wait()
}

type localKVHandler struct{}

func (localKVHandler) ReplicateKV(op *rpc.KVOp) error {
	db := database.LocalStore()
	if db == nil {
		return nil
	}
	switch op.Op {
	case "put":
		if err := db.PutSilent(op.Key, op.Value); err != nil {
			return err
		}
	case "del":
		if err := db.DeleteSilent(op.Key); err != nil {
			return err
		}
	}
	if bytes.Equal(op.Key, []byte(layoutCurrentKey)) {
		if cl := Global(); cl != nil {
			if err := cl.LoadLayout(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (localKVHandler) BulkSync(prefixes [][]byte, fn func(key, value []byte) bool) error {
	db := database.LocalStore()
	if db == nil {
		return nil
	}
	for _, p := range prefixes {
		it, err := db.NewIteratorWithPrefix(p)
		if err != nil {
			return err
		}
		for it.First(); it.Valid(); it.Next() {
			if !fn(it.Key(), it.Value()) {
				it.Close()
				return nil
			}
		}
		it.Close()
	}
	return nil
}

func PullBulkSyncFrom(ctx context.Context, pool *ConnPool, peer NodeID) (int, error) {
	cli, err := pool.Client(ctx, peer)
	if err != nil {
		return 0, err
	}

	prefixes := make([][]byte, len(ReplicatedPrefixes))
	for i, p := range ReplicatedPrefixes {
		prefixes[i] = append([]byte(nil), p...)
	}

	stream, err := cli.BulkSyncKV(ctx, &rpc.KVBulkRequest{Prefixes: prefixes})
	if err != nil {
		return 0, err
	}

	count := 0
	db := database.LocalStore()
	for {
		entry, err := stream.Recv()
		if err == io.EOF {
			return count, nil
		}
		if err != nil {
			return count, err
		}
		if db == nil {
			continue
		}
		if err := db.PutSilent(entry.Key, entry.Value); err != nil {
			return count, err
		}
		count++
	}
}
