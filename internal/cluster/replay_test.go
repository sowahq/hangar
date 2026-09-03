package cluster

import (
	"testing"
	"time"
)

func TestReplayGuard(t *testing.T) {
	g := newReplayGuard()
	now := time.Now()
	nonce := []byte("nonce-1")

	if !g.observe(nonce, now, HandshakeWindow) {
		t.Fatal("first observation must be accepted")
	}
	if g.observe(nonce, now.Add(time.Second), HandshakeWindow) {
		t.Fatal("replay within window must be rejected")
	}

	later := now.Add(2 * HandshakeWindow)
	if !g.observe([]byte("nonce-2"), later, HandshakeWindow) {
		t.Fatal("distinct nonce must be accepted")
	}
	if !g.observe(nonce, later, HandshakeWindow) {
		t.Fatal("nonce must be accepted again after the window elapsed")
	}
}
