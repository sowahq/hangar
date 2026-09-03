package cluster

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"storj.io/drpc/drpcconn"

	"github.com/sowahq/hangar/internal/api/rpc"
)

var (
	ErrAuthFailed      = errors.New("hmac handshake failed")
	ErrVersionMismatch = errors.New("protocol version mismatch")
	ErrPeerUnavailable = errors.New("peer unavailable")
)

const HandshakeWindow = 30 * time.Second

const ProtoVersion = rpc.ProtoVersion

type replayGuard struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newReplayGuard() *replayGuard {
	return &replayGuard{seen: make(map[string]time.Time)}
}

func (g *replayGuard) observe(nonce []byte, now time.Time, window time.Duration) bool {
	if g == nil || len(nonce) == 0 {
		return true
	}

	key := string(nonce)

	g.mu.Lock()
	defer g.mu.Unlock()

	for k, t := range g.seen {
		if now.Sub(t) > window {
			delete(g.seen, k)
		}
	}

	if _, ok := g.seen[key]; ok {
		return false
	}

	g.seen[key] = now
	return true
}

func SecretFingerprint(secret []byte) string {
	if len(secret) == 0 {
		return ""
	}
	sum := sha256.Sum256(secret)
	return fmt.Sprintf("%x", sum[:8])
}

func BuildHello(nodeID string, secret []byte) (*rpc.Hello, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ts := time.Now().UnixMilli()

	return &rpc.Hello{
		NodeId:       nodeID,
		Nonce:        nonce,
		Ts:           ts,
		Hmac:         helloMAC(secret, nodeID, nonce, ts),
		ProtoVersion: ProtoVersion,
	}, nil
}

func helloMAC(secret []byte, nodeID string, nonce []byte, ts int64) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(nodeID))
	mac.Write(nonce)

	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(ts))
	mac.Write(tsBuf[:])

	return mac.Sum(nil)
}

func VerifyHello(h *rpc.Hello, secret []byte, knownPeers map[string]struct{}, now time.Time) error {
	if h == nil || h.NodeId == "" || len(h.Nonce) == 0 || len(h.Hmac) == 0 {
		return ErrAuthFailed
	}

	if knownPeers != nil {
		if _, ok := knownPeers[h.NodeId]; !ok {
			return ErrAuthFailed
		}
	}

	skew := now.UnixMilli() - h.Ts
	if skew < 0 {
		skew = -skew
	}
	if time.Duration(skew)*time.Millisecond > HandshakeWindow {
		return ErrAuthFailed
	}

	expected := helloMAC(secret, h.NodeId, h.Nonce, h.Ts)
	if !hmac.Equal(expected, h.Hmac) {
		return ErrAuthFailed
	}

	peer := h.ProtoVersion
	if peer == 0 {
		peer = 1
	}
	if peer != ProtoVersion {
		return ErrVersionMismatch
	}

	return nil
}

func Dial(ctx context.Context, addr, nodeID string, secret []byte, tlsCfg *tls.Config) (*drpcconn.Conn, *rpc.HelloAck, error) {
	var d net.Dialer
	nc, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	if tlsCfg != nil {
		cfg := tlsCfg.Clone()
		if cfg.ServerName == "" {
			if host, _, _ := net.SplitHostPort(addr); host != "" {
				cfg.ServerName = host
			}
		}
		tc := tls.Client(nc, cfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = tc.Close()
			return nil, nil, fmt.Errorf("tls handshake: %w", err)
		}
		return handshakeOver(ctx, tc, nodeID, secret)
	}

	return handshakeOver(ctx, nc, nodeID, secret)
}

func DialTransport(ctx context.Context, tr net.Conn, nodeID string, secret []byte) (*drpcconn.Conn, *rpc.HelloAck, error) {
	return handshakeOver(ctx, tr, nodeID, secret)
}

func handshakeOver(ctx context.Context, tr net.Conn, nodeID string, secret []byte) (*drpcconn.Conn, *rpc.HelloAck, error) {
	conn := drpcconn.New(tr)

	hello, err := BuildHello(nodeID, secret)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	cli := rpc.NewDRPCClusterClient(conn)

	ack, err := cli.Handshake(ctx, hello)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}

	if ack == nil || !ack.Accepted {
		_ = conn.Close()
		reason := ""
		if ack != nil {
			reason = ack.Reason
		}
		if reason == "" {
			return nil, nil, ErrAuthFailed
		}
		return nil, nil, errors.New("handshake rejected: " + reason)
	}

	peer := ack.ProtoVersion
	if peer == 0 {
		peer = 1
	}
	if peer != ProtoVersion {
		_ = conn.Close()
		return nil, nil, ErrVersionMismatch
	}

	return conn, ack, nil
}
