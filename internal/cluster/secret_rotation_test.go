package cluster

import (
	"bytes"
	"testing"
	"time"
)

func TestVerifyHelloAcceptsPreviousSecret(t *testing.T) {
	oldSecret := testSecret(0x11)
	newSecret := testSecret(0x22)

	c := New(Config{
		NodeID:         "self",
		Listen:         "x:0",
		Secret:         newSecret,
		PreviousSecret: oldSecret,
		HeartbeatMS:    100,
	})

	helloOld, err := BuildHello("peer", oldSecret)
	if err != nil {
		t.Fatalf("build hello old: %v", err)
	}
	if err := c.VerifyHello(helloOld, time.Now()); err != nil {
		t.Fatalf("verify with previous secret: %v", err)
	}

	helloNew, err := BuildHello("peer", newSecret)
	if err != nil {
		t.Fatalf("build hello new: %v", err)
	}
	if err := c.VerifyHello(helloNew, time.Now()); err != nil {
		t.Fatalf("verify with primary secret: %v", err)
	}
}

func TestVerifyHelloRejectsUnrelatedSecret(t *testing.T) {
	c := New(Config{
		NodeID:         "self",
		Listen:         "x:0",
		Secret:         testSecret(0x22),
		PreviousSecret: testSecret(0x11),
		HeartbeatMS:    100,
	})

	hello, _ := BuildHello("peer", testSecret(0x99))
	if err := c.VerifyHello(hello, time.Now()); err == nil {
		t.Fatalf("expected rejection for unrelated secret")
	}
}

func TestVerifyHelloNoPreviousFallsThrough(t *testing.T) {
	c := New(Config{
		NodeID:      "self",
		Listen:      "x:0",
		Secret:      testSecret(0x22),
		HeartbeatMS: 100,
	})

	hello, _ := BuildHello("peer", testSecret(0x11))
	if err := c.VerifyHello(hello, time.Now()); err == nil {
		t.Fatalf("expected rejection when no previous and primary mismatch")
	}
}

func TestVerifyLayoutAcceptsPreviousSecret(t *testing.T) {
	oldSecret := testSecret(0x11)
	newSecret := testSecret(0x22)

	c := New(Config{
		NodeID:         "self",
		Listen:         "x:0",
		Secret:         newSecret,
		PreviousSecret: oldSecret,
		HeartbeatMS:    100,
	})

	l := &Layout{Version: 1, Nodes: []LayoutNode{{ID: "n1", Addr: "n1:0"}}}
	l.Sign(oldSecret)

	if err := c.verifyLayout(l); err != nil {
		t.Fatalf("verifyLayout with previous secret: %v", err)
	}

	l2 := &Layout{Version: 2, Nodes: []LayoutNode{{ID: "n1", Addr: "n1:0"}}}
	l2.Sign(newSecret)
	if err := c.verifyLayout(l2); err != nil {
		t.Fatalf("verifyLayout with primary secret: %v", err)
	}
}

func TestVerifyLayoutRejectsUnrelatedSecret(t *testing.T) {
	c := New(Config{
		NodeID:         "self",
		Listen:         "x:0",
		Secret:         testSecret(0x22),
		PreviousSecret: testSecret(0x11),
		HeartbeatMS:    100,
	})

	l := &Layout{Version: 1, Nodes: []LayoutNode{{ID: "n1", Addr: "n1:0"}}}
	l.Sign(testSecret(0x99))
	if err := c.verifyLayout(l); err == nil {
		t.Fatalf("expected rejection for unrelated layout signer")
	}
}

func TestSecretFingerprintDeterministic(t *testing.T) {
	a := SecretFingerprint([]byte{1, 2, 3, 4})
	b := SecretFingerprint([]byte{1, 2, 3, 4})
	if a != b {
		t.Fatalf("non-deterministic fingerprint: %s vs %s", a, b)
	}
	if a == "" {
		t.Fatalf("empty fingerprint for non-empty secret")
	}
	if SecretFingerprint(nil) != "" {
		t.Fatalf("nil secret must yield empty fingerprint")
	}
	if c := SecretFingerprint([]byte{5, 6, 7, 8}); c == a {
		t.Fatalf("different secrets must yield different fingerprints")
	}
}

func TestSecretFingerprintLength(t *testing.T) {
	fp := SecretFingerprint(bytes.Repeat([]byte{0xAB}, 32))
	if len(fp) != 16 {
		t.Fatalf("fingerprint length=%d want 16 (8 bytes hex)", len(fp))
	}
}
