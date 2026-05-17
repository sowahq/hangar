package rpc

import (
	"bytes"
	"context"
	"errors"
	"io"
)

const chunkStreamFrameSize = 64 * 1024

func (s *Server) PutMetadata(ctx context.Context, op *MetadataOp) (*MetadataAck, error) {
	if s.Metadata == nil {
		return s.DRPCClusterUnimplementedServer.PutMetadata(ctx, op)
	}
	if op == nil {
		return &MetadataAck{Ok: false, Error: "nil op"}, nil
	}
	if err := s.Metadata.PutMetadata(op.Bucket, op.Key, op.Metadata); err != nil {
		return &MetadataAck{Ok: false, Error: err.Error()}, nil
	}
	return &MetadataAck{Ok: true}, nil
}

func (s *Server) GetMetadata(ctx context.Context, k *MetadataKey) (*Metadata, error) {
	if s.Metadata == nil {
		return s.DRPCClusterUnimplementedServer.GetMetadata(ctx, k)
	}
	if k == nil {
		return &Metadata{Found: false}, nil
	}
	raw, found, err := s.Metadata.GetMetadata(k.Bucket, k.Key)
	if err != nil {
		return nil, err
	}
	return &Metadata{Found: found, Metadata: raw}, nil
}

func (s *Server) DeleteMetadata(ctx context.Context, k *MetadataKey) (*Metadata, error) {
	if s.Metadata == nil {
		return s.DRPCClusterUnimplementedServer.DeleteMetadata(ctx, k)
	}
	if k == nil {
		return &Metadata{Found: false}, nil
	}
	prev, found, err := s.Metadata.DeleteMetadata(k.Bucket, k.Key)
	if err != nil {
		return nil, err
	}
	return &Metadata{Found: found, Metadata: prev}, nil
}

func (s *Server) ListMetadata(req *ListRequest, stream DRPCCluster_ListMetadataStream) error {
	if s.Metadata == nil {
		return s.DRPCClusterUnimplementedServer.ListMetadata(req, stream)
	}
	if req == nil {
		return nil
	}

	prefix := "metadata:"
	if req.Bucket != "" {
		prefix += req.Bucket + "/"
	}
	if req.Prefix != "" {
		prefix += req.Prefix
	}

	var sendErr error
	err := s.Metadata.ListMetadata(prefix, func(k, v []byte) bool {
		key := string(k)
		bucket := req.Bucket
		objKey := key
		if bytes.HasPrefix(k, []byte("metadata:")) {
			rest := key[len("metadata:"):]
			if bucket != "" && bytes.HasPrefix([]byte(rest), []byte(bucket+"/")) {
				objKey = rest[len(bucket)+1:]
			} else {
				objKey = rest
			}
		}
		if err := stream.Send(&MetadataEntry{Bucket: bucket, Key: objKey, Metadata: v}); err != nil {
			sendErr = err
			return false
		}
		return true
	})
	if sendErr != nil {
		return sendErr
	}
	return err
}

func (s *Server) HasChunk(ctx context.Context, ref *ChunkRef) (*ChunkPresence, error) {
	if s.Chunks == nil {
		return s.DRPCClusterUnimplementedServer.HasChunk(ctx, ref)
	}
	if ref == nil {
		return &ChunkPresence{Present: false}, nil
	}
	ok, err := s.Chunks.HasChunk(ref.Hash)
	if err != nil {
		return nil, err
	}
	return &ChunkPresence{Hash: ref.Hash, ShardIdx: ref.ShardIdx, Present: ok}, nil
}

func (s *Server) HasChunks(ctx context.Context, batch *ChunkRefBatch) (*ChunkPresenceBatch, error) {
	if s.Chunks == nil {
		return s.DRPCClusterUnimplementedServer.HasChunks(ctx, batch)
	}
	if batch == nil {
		return &ChunkPresenceBatch{}, nil
	}
	out := &ChunkPresenceBatch{Entries: make([]*ChunkPresence, 0, len(batch.Refs))}
	for _, r := range batch.Refs {
		ok, err := s.Chunks.HasChunk(r.Hash)
		if err != nil {
			return nil, err
		}
		out.Entries = append(out.Entries, &ChunkPresence{Hash: r.Hash, ShardIdx: r.ShardIdx, Present: ok})
	}
	return out, nil
}

func (s *Server) PutChunk(stream DRPCCluster_PutChunkStream) error {
	if s.Chunks == nil {
		return s.DRPCClusterUnimplementedServer.PutChunk(stream)
	}

	var hash string
	var shard int32
	var buf bytes.Buffer

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hash == "" {
			hash = msg.Hash
			shard = msg.ShardIdx
		}
		if len(msg.Payload) > 0 {
			buf.Write(msg.Payload)
		}
		if msg.Last {
			break
		}
	}

	if hash == "" {
		return stream.SendAndClose(&PutChunkAck{Stored: false, Error: "empty stream"})
	}
	if err := s.Chunks.PutChunk(hash, buf.Bytes()); err != nil {
		return stream.SendAndClose(&PutChunkAck{Stored: false, Error: err.Error()})
	}
	_ = shard
	return stream.SendAndClose(&PutChunkAck{Stored: true})
}

func (s *Server) GetChunk(ref *ChunkRef, stream DRPCCluster_GetChunkStream) error {
	if s.Chunks == nil {
		return s.DRPCClusterUnimplementedServer.GetChunk(ref, stream)
	}
	if ref == nil {
		return errors.New("nil ref")
	}
	rc, err := s.Chunks.OpenChunk(ref.Hash)
	if err != nil {
		return err
	}
	defer rc.Close()

	buf := make([]byte, chunkStreamFrameSize)
	for {
		n, rerr := rc.Read(buf)
		if n > 0 {
			payload := make([]byte, n)
			copy(payload, buf[:n])
			if err := stream.Send(&ChunkData{
				Hash:     ref.Hash,
				ShardIdx: ref.ShardIdx,
				Payload:  payload,
				Last:     rerr == io.EOF,
			}); err != nil {
				return err
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

func (s *Server) DeleteChunkReplica(ctx context.Context, ref *ChunkRef) (*Ack, error) {
	if s.Chunks == nil {
		return s.DRPCClusterUnimplementedServer.DeleteChunkReplica(ctx, ref)
	}
	if ref == nil {
		return &Ack{Ok: false, Error: "nil ref"}, nil
	}
	if err := s.Chunks.DeleteChunkReplica(ref.Hash); err != nil {
		return &Ack{Ok: false, Error: err.Error()}, nil
	}
	return &Ack{Ok: true}, nil
}

func (s *Server) IncRefs(ctx context.Context, d *RefDelta) (*Ack, error) {
	if s.Refs == nil {
		return s.DRPCClusterUnimplementedServer.IncRefs(ctx, d)
	}
	if d == nil {
		return &Ack{Ok: true}, nil
	}
	if err := s.Refs.IncRefs(d.Hashes); err != nil {
		return &Ack{Ok: false, Error: err.Error()}, nil
	}
	return &Ack{Ok: true}, nil
}

func (s *Server) DecRefs(ctx context.Context, d *RefDelta) (*Ack, error) {
	if s.Refs == nil {
		return s.DRPCClusterUnimplementedServer.DecRefs(ctx, d)
	}
	if d == nil {
		return &Ack{Ok: true}, nil
	}
	if err := s.Refs.DecRefs(d.Hashes); err != nil {
		return &Ack{Ok: false, Error: err.Error()}, nil
	}
	return &Ack{Ok: true}, nil
}

func (s *Server) GetLayout(ctx context.Context, req *LayoutRequest) (*LayoutResponse, error) {
	if s.Layout == nil {
		return s.DRPCClusterUnimplementedServer.GetLayout(ctx, req)
	}
	if req == nil {
		return &LayoutResponse{}, nil
	}
	raw, err := s.Layout.GetLayout(req.Version)
	if err != nil {
		return nil, err
	}
	return &LayoutResponse{SignedLayout: raw}, nil
}
