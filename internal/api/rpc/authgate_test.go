package rpc

import (
	"context"
	"errors"
	"testing"

	"storj.io/drpc"
	"storj.io/drpc/drpcctx"
	"storj.io/drpc/drpcerr"
)

type fakeTransport struct{}

func (fakeTransport) Read([]byte) (int, error)  { return 0, nil }
func (fakeTransport) Write([]byte) (int, error) { return 0, nil }
func (fakeTransport) Close() error              { return nil }

type fakeStream struct {
	drpc.Stream
	ctx context.Context
}

func (s fakeStream) Context() context.Context { return s.ctx }

type recordingHandler struct{ called bool }

func (h *recordingHandler) HandleRPC(drpc.Stream, string) error {
	h.called = true
	return nil
}

func streamWithTransport(tr drpc.Transport) fakeStream {
	ctx := drpcctx.WithTransport(context.Background(), tr)
	return fakeStream{ctx: ctx}
}

func TestAuthGate(t *testing.T) {
	tr := fakeTransport{}

	tests := []struct {
		name       string
		rpc        string
		mark       bool
		wantCalled bool
		wantErr    bool
	}{
		{name: "handshake bypasses gate", rpc: handshakeRPC, mark: false, wantCalled: true, wantErr: false},
		{name: "unauthenticated rpc rejected", rpc: "/hangar.cluster.v1.Cluster/PutChunk", mark: false, wantCalled: false, wantErr: true},
		{name: "authenticated rpc allowed", rpc: "/hangar.cluster.v1.Cluster/PutChunk", mark: true, wantCalled: true, wantErr: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			auth := NewConnAuth()
			if tc.mark {
				auth.Mark(tr)
			}

			inner := &recordingHandler{}
			gate := NewAuthGate(inner, auth)

			err := gate.HandleRPC(streamWithTransport(tr), tc.rpc)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				if drpcerr.Code(err) != codeUnauthenticated {
					t.Fatalf("want code %d, got %d", codeUnauthenticated, drpcerr.Code(err))
				}
			} else if err != nil {
				t.Fatalf("want nil error, got %v", err)
			}

			if inner.called != tc.wantCalled {
				t.Fatalf("inner called = %v, want %v", inner.called, tc.wantCalled)
			}
		})
	}
}

func TestConnAuthForget(t *testing.T) {
	auth := NewConnAuth()
	tr := fakeTransport{}

	auth.Mark(tr)
	if !auth.authorized(tr) {
		t.Fatal("want authorized after Mark")
	}

	auth.Forget(tr)
	if auth.authorized(tr) {
		t.Fatal("want unauthorized after Forget")
	}
}

func TestAuthGateMissingTransport(t *testing.T) {
	auth := NewConnAuth()
	inner := &recordingHandler{}
	gate := NewAuthGate(inner, auth)

	err := gate.HandleRPC(fakeStream{ctx: context.Background()}, "/hangar.cluster.v1.Cluster/PutChunk")
	if err == nil {
		t.Fatal("want error when transport missing")
	}
	if !errors.Is(err, errUnauthenticated) {
		t.Fatalf("want errUnauthenticated, got %v", err)
	}
	if inner.called {
		t.Fatal("inner must not be called without transport")
	}
}
