package cluster

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"storj.io/drpc/drpcmux"
	"storj.io/drpc/drpcserver"

	"github.com/anhostfr/hangar/internal/api/rpc"
)

func testSecret(b byte) []byte {
	s := make([]byte, 32)
	for i := range s {
		s[i] = b
	}
	return s
}

func TestBuildAndVerifyHello(t *testing.T) {
	secret := testSecret(0x42)
	peers := map[string]struct{}{"n1": {}}

	hello, err := BuildHello("n1", secret)
	if err != nil {
		t.Fatalf("BuildHello: %v", err)
	}

	if hello.NodeId != "n1" || len(hello.Nonce) != 16 || len(hello.Hmac) != 32 {
		t.Fatalf("hello shape wrong: %+v", hello)
	}

	if err := VerifyHello(hello, secret, peers, time.Now()); err != nil {
		t.Fatalf("roundtrip Verify: %v", err)
	}
}

func TestVerifyHello(t *testing.T) {
	secret := testSecret(0x42)
	otherSecret := testSecret(0x11)
	peers := map[string]struct{}{"n1": {}}

	mkHello := func(t *testing.T, id string, sec []byte, ts int64) *rpc.Hello {
		t.Helper()
		h, err := BuildHello(id, sec)
		if err != nil {
			t.Fatalf("BuildHello: %v", err)
		}
		if ts != 0 {
			h.Ts = ts
			h.Hmac = helloMAC(sec, id, h.Nonce, ts)
		}
		return h
	}

	now := time.Now()

	cases := []struct {
		name   string
		hello  func(*testing.T) *rpc.Hello
		peers  map[string]struct{}
		secret []byte
		now    time.Time
		want   error
	}{
		{
			name:   "good",
			hello:  func(t *testing.T) *rpc.Hello { return mkHello(t, "n1", secret, 0) },
			peers:  peers,
			secret: secret,
			now:    now,
		},
		{
			name:   "nil hello",
			hello:  func(*testing.T) *rpc.Hello { return nil },
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name: "empty node id",
			hello: func(t *testing.T) *rpc.Hello {
				h := mkHello(t, "n1", secret, 0)
				h.NodeId = ""
				return h
			},
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name: "empty nonce",
			hello: func(t *testing.T) *rpc.Hello {
				h := mkHello(t, "n1", secret, 0)
				h.Nonce = nil
				return h
			},
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name: "empty hmac",
			hello: func(t *testing.T) *rpc.Hello {
				h := mkHello(t, "n1", secret, 0)
				h.Hmac = nil
				return h
			},
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name:   "unknown peer",
			hello:  func(t *testing.T) *rpc.Hello { return mkHello(t, "n2", secret, 0) },
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name:   "wrong secret",
			hello:  func(t *testing.T) *rpc.Hello { return mkHello(t, "n1", otherSecret, 0) },
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name: "tampered hmac",
			hello: func(t *testing.T) *rpc.Hello {
				h := mkHello(t, "n1", secret, 0)
				h.Hmac[0] ^= 0xFF
				return h
			},
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name:   "ts too old",
			hello:  func(t *testing.T) *rpc.Hello { return mkHello(t, "n1", secret, now.Add(-31*time.Second).UnixMilli()) },
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name:   "ts too far future",
			hello:  func(t *testing.T) *rpc.Hello { return mkHello(t, "n1", secret, now.Add(31*time.Second).UnixMilli()) },
			peers:  peers,
			secret: secret,
			now:    now,
			want:   ErrAuthFailed,
		},
		{
			name:   "ts within window",
			hello:  func(t *testing.T) *rpc.Hello { return mkHello(t, "n1", secret, now.Add(-20*time.Second).UnixMilli()) },
			peers:  peers,
			secret: secret,
			now:    now,
		},
		{
			name:   "nil peers allows any",
			hello:  func(t *testing.T) *rpc.Hello { return mkHello(t, "n9", secret, 0) },
			peers:  nil,
			secret: secret,
			now:    now,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := VerifyHello(tc.hello(t), tc.secret, tc.peers, tc.now)
			if err != tc.want {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

type handshakeImpl struct {
	rpc.DRPCClusterUnimplementedServer

	secret []byte
	peers  map[string]struct{}

	viewVersion   uint64
	layoutVersion uint64

	reject bool
}

func (h *handshakeImpl) Handshake(ctx context.Context, hello *rpc.Hello) (*rpc.HelloAck, error) {
	if h.reject {
		return &rpc.HelloAck{Accepted: false, Reason: "rejected by server"}, nil
	}
	if err := VerifyHello(hello, h.secret, h.peers, time.Now()); err != nil {
		return &rpc.HelloAck{Accepted: false, Reason: err.Error()}, nil
	}
	return &rpc.HelloAck{
		Accepted:      true,
		ViewVersion:   h.viewVersion,
		LayoutVersion: h.layoutVersion,
	}, nil
}

func startHandshakeServer(t *testing.T, impl *handshakeImpl) (clientSide net.Conn, stop func()) {
	t.Helper()

	mux := drpcmux.New()
	if err := rpc.DRPCRegisterCluster(mux, impl); err != nil {
		t.Fatalf("register: %v", err)
	}

	srv := drpcserver.New(mux)

	c, s := net.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.ServeOne(ctx, s)
	}()

	stop = func() {
		cancel()
		_ = s.Close()
		_ = c.Close()
		wg.Wait()
	}
	return c, stop
}

func TestDialHandshakeSuccess(t *testing.T) {
	secret := testSecret(0x42)
	peers := map[string]struct{}{"n1": {}}

	impl := &handshakeImpl{
		secret:        secret,
		peers:         peers,
		viewVersion:   7,
		layoutVersion: 3,
	}
	cli, stop := startHandshakeServer(t, impl)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, ack, err := DialTransport(ctx, cli, "n1", secret)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	if !ack.Accepted || ack.ViewVersion != 7 || ack.LayoutVersion != 3 {
		t.Fatalf("ack mismatch: %+v", ack)
	}
}

func TestDialHandshakeRejected(t *testing.T) {
	secret := testSecret(0x42)
	wrong := testSecret(0x11)
	peers := map[string]struct{}{"n1": {}}

	impl := &handshakeImpl{secret: secret, peers: peers}
	cli, stop := startHandshakeServer(t, impl)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := DialTransport(ctx, cli, "n1", wrong)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

func TestDialHandshakeServerReject(t *testing.T) {
	secret := testSecret(0x42)
	peers := map[string]struct{}{"n1": {}}

	impl := &handshakeImpl{secret: secret, peers: peers, reject: true}
	cli, stop := startHandshakeServer(t, impl)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := DialTransport(ctx, cli, "n1", secret)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}
