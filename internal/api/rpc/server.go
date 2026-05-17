package rpc

import (
	"context"
	"io"
	"time"
)

type Bridge interface {
	VerifyHello(h *Hello, now time.Time) error
	BuildHeartbeat() *Heartbeat
	OnHeartbeat(h *Heartbeat)
	ViewVersion() uint64
	LayoutVersion() uint64
}

type MetadataHandler interface {
	PutMetadata(bucket, key string, raw []byte) error
	GetMetadata(bucket, key string) ([]byte, bool, error)
	DeleteMetadata(bucket, key string) ([]byte, bool, error)
	ListMetadata(prefix string, fn func(key, val []byte) bool) error
}

type ChunkHandler interface {
	PutChunk(hash string, payload []byte) error
	OpenChunk(hash string) (io.ReadCloser, error)
	HasChunk(hash string) (bool, error)
	DeleteChunkReplica(hash string) error
}

type RefcountHandler interface {
	IncRefs(hashes []string) error
	DecRefs(hashes []string) error
}

type LayoutHandler interface {
	GetLayout(version uint64) ([]byte, error)
}

type Server struct {
	DRPCClusterUnimplementedServer

	bridge   Bridge
	Metadata MetadataHandler
	Chunks   ChunkHandler
	Refs     RefcountHandler
	Layout   LayoutHandler
}

func NewServer(b Bridge) *Server {
	return &Server{bridge: b}
}

func (s *Server) Handshake(ctx context.Context, h *Hello) (*HelloAck, error) {
	if s.bridge == nil {
		return s.DRPCClusterUnimplementedServer.Handshake(ctx, h)
	}

	if err := s.bridge.VerifyHello(h, time.Now()); err != nil {
		return &HelloAck{Accepted: false, Reason: err.Error()}, nil
	}

	return &HelloAck{
		Accepted:      true,
		ViewVersion:   s.bridge.ViewVersion(),
		LayoutVersion: s.bridge.LayoutVersion(),
	}, nil
}

func (s *Server) HeartbeatStream(stream DRPCCluster_HeartbeatStreamStream) error {
	if s.bridge == nil {
		return s.DRPCClusterUnimplementedServer.HeartbeatStream(stream)
	}

	for {
		hb, err := stream.Recv()
		if err != nil {
			return err
		}

		s.bridge.OnHeartbeat(hb)

		if err := stream.Send(s.bridge.BuildHeartbeat()); err != nil {
			return err
		}
	}
}

var _ DRPCClusterServer = (*Server)(nil)
