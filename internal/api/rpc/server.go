package rpc

import (
	"context"
	"time"
)

type Bridge interface {
	VerifyHello(h *Hello, now time.Time) error
	BuildHeartbeat() *Heartbeat
	OnHeartbeat(h *Heartbeat)
	ViewVersion() uint64
	LayoutVersion() uint64
}

type Server struct {
	DRPCClusterUnimplementedServer

	bridge Bridge
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
