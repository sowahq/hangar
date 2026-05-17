package cluster

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/anhostfr/hangar/internal/api/rpc"
	"github.com/anhostfr/hangar/internal/storage"
)

func newOpID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

const (
	chunkRF         = 2
	chunkRPCTimeout = 30 * time.Second
)

type ClusteredChunkStore struct {
	cl   *Cluster
	pool *ConnPool

	local storage.LocalChunkStore
}

func NewClusteredChunkStore(cl *Cluster, pool *ConnPool) *ClusteredChunkStore {
	return &ClusteredChunkStore{cl: cl, pool: pool}
}

func (s *ClusteredChunkStore) owners(hash string) []NodeID {
	owners := s.cl.ChunkOwners(hash, chunkRF)
	if len(owners) == 0 {
		return []NodeID{s.cl.Self()}
	}
	return owners
}

func (s *ClusteredChunkStore) PutRaw(hash string, payload []byte) error {
	owners := s.owners(hash)

	stored := 0
	var lastErr error
	for _, id := range owners {
		if id == s.cl.Self() {
			if err := s.local.PutRaw(hash, payload); err != nil {
				lastErr = err
				continue
			}
			stored++
			continue
		}
		if err := s.putRemote(id, hash, payload); err != nil {
			lastErr = err
			continue
		}
		stored++
	}

	if stored == 0 {
		if lastErr == nil {
			lastErr = errors.New("no owners stored chunk")
		}
		return lastErr
	}
	return nil
}

func (s *ClusteredChunkStore) putRemote(id NodeID, hash string, payload []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), chunkRPCTimeout)
	defer cancel()

	cli, err := s.pool.Client(ctx, id)
	if err != nil {
		return err
	}

	stream, err := cli.PutChunk(ctx)
	if err != nil {
		return err
	}

	const frame = 256 * 1024
	for off := 0; off < len(payload); off += frame {
		end := off + frame
		if end > len(payload) {
			end = len(payload)
		}
		last := end == len(payload)
		if err := stream.Send(&rpc.ChunkData{Hash: hash, Payload: payload[off:end], Last: last}); err != nil {
			return err
		}
	}
	if len(payload) == 0 {
		if err := stream.Send(&rpc.ChunkData{Hash: hash, Last: true}); err != nil {
			return err
		}
	}
	ack, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}
	if !ack.Stored {
		return errors.New("remote chunk not stored: " + ack.Error)
	}
	return nil
}

func (s *ClusteredChunkStore) OpenRaw(hash string) (io.ReadCloser, error) {
	owners := s.owners(hash)
	var lastErr error
	for _, id := range owners {
		rc, err := s.openFrom(id, hash)
		if err == nil {
			return rc, nil
		}
		if errors.Is(err, storage.ErrChunkNotFound) {
			lastErr = err
			continue
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = storage.ErrChunkNotFound
	}
	return nil, lastErr
}

func (s *ClusteredChunkStore) openFrom(id NodeID, hash string) (io.ReadCloser, error) {
	if id == s.cl.Self() {
		return s.local.OpenRaw(hash)
	}

	ctx, cancel := context.WithTimeout(context.Background(), chunkRPCTimeout)
	cli, err := s.pool.Client(ctx, id)
	if err != nil {
		cancel()
		return nil, err
	}

	stream, err := cli.GetChunk(ctx, &rpc.ChunkRef{Hash: hash})
	if err != nil {
		cancel()
		return nil, err
	}

	var buf bytes.Buffer
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			cancel()
			return nil, err
		}
		if len(msg.Payload) > 0 {
			buf.Write(msg.Payload)
		}
		if msg.Last {
			break
		}
	}
	cancel()
	if buf.Len() == 0 {
		return nil, storage.ErrChunkNotFound
	}
	return io.NopCloser(&buf), nil
}

func (s *ClusteredChunkStore) Exists(hash string) (bool, error) {
	owners := s.owners(hash)
	for _, id := range owners {
		ok, err := s.existsAt(id, hash)
		if err != nil {
			continue
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func (s *ClusteredChunkStore) existsAt(id NodeID, hash string) (bool, error) {
	if id == s.cl.Self() {
		return s.local.Exists(hash)
	}
	ctx, cancel := context.WithTimeout(context.Background(), chunkRPCTimeout)
	defer cancel()
	cli, err := s.pool.Client(ctx, id)
	if err != nil {
		return false, err
	}
	pres, err := cli.HasChunk(ctx, &rpc.ChunkRef{Hash: hash})
	if err != nil {
		return false, err
	}
	return pres.Present, nil
}

func (s *ClusteredChunkStore) Delete(hash string) error {
	owners := s.owners(hash)
	for _, id := range owners {
		if id == s.cl.Self() {
			_ = s.local.Delete(hash)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), chunkRPCTimeout)
		cli, err := s.pool.Client(ctx, id)
		if err != nil {
			cancel()
			continue
		}
		_, _ = cli.DeleteChunkReplica(ctx, &rpc.ChunkRef{Hash: hash})
		cancel()
	}
	return nil
}

type ClusteredRefcountStore struct {
	cl   *Cluster
	pool *ConnPool

	local storage.LocalRefcountStore
}

func NewClusteredRefcountStore(cl *Cluster, pool *ConnPool) *ClusteredRefcountStore {
	return &ClusteredRefcountStore{cl: cl, pool: pool}
}

func (s *ClusteredRefcountStore) IncRefs(hashes []string) error {
	return s.deltaByOwner(hashes, true)
}

func (s *ClusteredRefcountStore) DecRefs(hashes []string) error {
	return s.deltaByOwner(hashes, false)
}

func (s *ClusteredRefcountStore) Referenced(hash string) (bool, error) {
	return s.local.Referenced(hash)
}

func (s *ClusteredRefcountStore) deltaByOwner(hashes []string, inc bool) error {
	byNode := map[NodeID][]string{}
	for _, h := range hashes {
		for _, owner := range s.cl.ChunkOwners(h, chunkRF) {
			byNode[owner] = append(byNode[owner], h)
		}
	}

	opID := newOpID()

	for id, hs := range byNode {
		if id == s.cl.Self() {
			_ = storage.ApplyRefOp(opID, inc, hs)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), metadataRPCWait)
		cli, err := s.pool.Client(ctx, id)
		if err != nil {
			cancel()
			continue
		}
		if inc {
			_, _ = cli.IncRefs(ctx, &rpc.RefDelta{OpId: opID, Hashes: hs})
		} else {
			_, _ = cli.DecRefs(ctx, &rpc.RefDelta{OpId: opID, Hashes: hs})
		}
		cancel()
	}
	return nil
}
