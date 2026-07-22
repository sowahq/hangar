package cluster

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/cockroachdb/pebble"

	"github.com/sowahq/hangar/internal/api/rpc"
	"github.com/sowahq/hangar/internal/storage"
)

const (
	metadataRF      = 2
	metadataRPCWait = 5 * time.Second
)

type ClusteredMetadataStore struct {
	cl   *Cluster
	pool *ConnPool

	local storage.LocalMetadataStore
}

func NewClusteredMetadataStore(cl *Cluster, pool *ConnPool) *ClusteredMetadataStore {
	return &ClusteredMetadataStore{cl: cl, pool: pool}
}

func (s *ClusteredMetadataStore) owners(bucket, key string) []NodeID {
	owners := s.cl.ObjectShardReplicas(bucket, key, metadataRF)
	if len(owners) == 0 {
		return []NodeID{s.cl.Self()}
	}
	return owners
}

func (s *ClusteredMetadataStore) PutRaw(bucket, key string, raw []byte) error {
	owners := s.owners(bucket, key)
	primary := owners[0]

	if primary == s.cl.Self() {
		if err := s.local.PutRaw(bucket, key, raw); err != nil {
			return err
		}
		_ = AppendWAL("put", bucket, key, raw)
		s.replicateAsync(owners[1:], bucket, key, raw)
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), metadataRPCWait)
	defer cancel()

	cli, err := s.pool.Client(ctx, primary)
	if err != nil {
		return err
	}
	ack, err := cli.PutMetadata(ctx, &rpc.MetadataOp{Bucket: bucket, Key: key, Metadata: raw})
	if err != nil {
		return err
	}
	if !ack.Ok {
		return errors.New("primary put metadata: " + ack.Error)
	}
	s.replicateAsync(owners[1:], bucket, key, raw)
	return nil
}

func (s *ClusteredMetadataStore) replicateAsync(targets []NodeID, bucket, key string, raw []byte) {
	for _, id := range targets {
		if id == s.cl.Self() {
			_ = s.local.PutRaw(bucket, key, raw)
			continue
		}
		go func(id NodeID) {
			ctx, cancel := context.WithTimeout(context.Background(), metadataRPCWait)
			defer cancel()
			cli, err := s.pool.Client(ctx, id)
			if err != nil {
				return
			}
			_, _ = cli.PutMetadata(ctx, &rpc.MetadataOp{Bucket: bucket, Key: key, Metadata: raw})
		}(id)
	}
}

func (s *ClusteredMetadataStore) GetRaw(bucket, key string) ([]byte, error) {
	owners := s.owners(bucket, key)
	var lastErr error
	notFoundCount := 0
	for _, id := range owners {
		raw, err := s.getFrom(id, bucket, key)
		if err == nil {
			return raw, nil
		}
		if errors.Is(err, pebble.ErrNotFound) {
			notFoundCount++
			continue
		}
		lastErr = err
	}
	if notFoundCount == len(owners) {
		return nil, pebble.ErrNotFound
	}
	if lastErr == nil {
		return nil, pebble.ErrNotFound
	}
	return nil, lastErr
}

func (s *ClusteredMetadataStore) getFrom(id NodeID, bucket, key string) ([]byte, error) {
	if id == s.cl.Self() {
		return s.local.GetRaw(bucket, key)
	}
	ctx, cancel := context.WithTimeout(context.Background(), metadataRPCWait)
	defer cancel()
	cli, err := s.pool.Client(ctx, id)
	if err != nil {
		return nil, err
	}
	meta, err := cli.GetMetadata(ctx, &rpc.MetadataKey{Bucket: bucket, Key: key})
	if err != nil {
		return nil, err
	}
	if !meta.Found {
		return nil, pebble.ErrNotFound
	}
	return meta.Metadata, nil
}

func (s *ClusteredMetadataStore) DeleteRaw(bucket, key string) ([]byte, error) {
	owners := s.owners(bucket, key)
	primary := owners[0]

	var prev []byte
	var primaryErr error
	if primary == s.cl.Self() {
		prev, primaryErr = s.local.DeleteRaw(bucket, key)
		if primaryErr == nil {
			_ = AppendWAL("del", bucket, key, nil)
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), metadataRPCWait)
		cli, err := s.pool.Client(ctx, primary)
		if err != nil {
			cancel()
			return nil, err
		}
		meta, err := cli.DeleteMetadata(ctx, &rpc.MetadataKey{Bucket: bucket, Key: key})
		cancel()
		if err != nil {
			return nil, err
		}
		if meta.Found {
			prev = meta.Metadata
		} else {
			primaryErr = pebble.ErrNotFound
		}
	}

	for _, id := range owners[1:] {
		go func(id NodeID) {
			if id == s.cl.Self() {
				_, _ = s.local.DeleteRaw(bucket, key)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), metadataRPCWait)
			defer cancel()
			cli, err := s.pool.Client(ctx, id)
			if err != nil {
				return
			}
			_, _ = cli.DeleteMetadata(ctx, &rpc.MetadataKey{Bucket: bucket, Key: key})
		}(id)
	}

	if primaryErr != nil {
		return nil, primaryErr
	}
	return prev, nil
}

func (s *ClusteredMetadataStore) ListRaw(prefix string, fn func(key, val []byte) bool) error {
	seen := map[string]struct{}{}
	stop := false
	emit := func(k, v []byte) bool {
		if stop {
			return false
		}
		ks := string(k)
		if _, dup := seen[ks]; dup {
			return true
		}
		seen[ks] = struct{}{}
		if !fn(k, v) {
			stop = true
			return false
		}
		return true
	}

	if err := s.local.ListRaw(prefix, emit); err != nil {
		return err
	}
	if stop {
		return nil
	}

	view := s.cl.View()
	for id, ns := range view.Nodes {
		if id == s.cl.Self() || ns.Status != StatusActive {
			continue
		}
		if err := s.listRemote(id, prefix, emit); err != nil {
			continue
		}
		if stop {
			return nil
		}
	}
	return nil
}

func (s *ClusteredMetadataStore) listRemote(id NodeID, prefix string, emit func(k, v []byte) bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), metadataRPCWait)
	defer cancel()

	cli, err := s.pool.Client(ctx, id)
	if err != nil {
		return err
	}

	stream, err := cli.ListMetadata(ctx, &rpc.ListRequest{Prefix: prefix})
	if err != nil {
		return err
	}

	for {
		entry, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		fullKey := "metadata:"
		if entry.Bucket != "" {
			fullKey += entry.Bucket + "/"
		}
		fullKey += entry.Key
		if !emit([]byte(fullKey), entry.Metadata) {
			return nil
		}
	}
}
