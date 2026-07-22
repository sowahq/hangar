package cluster

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"storj.io/drpc/drpcmux"
	"storj.io/drpc/drpcserver"

	"github.com/sowahq/hangar/internal/api/rpc"
)

func writeSelfSignedTLS(t *testing.T) (certPath, keyPath, caPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "hangar-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("sign cert: %v", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	dir := t.TempDir()
	certPath = filepath.Join(dir, "node.crt")
	keyPath = filepath.Join(dir, "node.key")
	caPath = certPath

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath, caPath
}

func TestBuildTLSConfigs(t *testing.T) {
	cert, key, ca := writeSelfSignedTLS(t)

	cases := []struct {
		name      string
		opt       TLSOptions
		wantNil   bool
		wantErr   bool
		wantMTLS  bool
	}{
		{name: "disabled", opt: TLSOptions{}, wantNil: true},
		{name: "missing key", opt: TLSOptions{CertFile: cert}, wantErr: true},
		{name: "missing cert", opt: TLSOptions{KeyFile: key}, wantErr: true},
		{name: "cert+key no ca", opt: TLSOptions{CertFile: cert, KeyFile: key}},
		{name: "cert+key+ca mtls", opt: TLSOptions{CertFile: cert, KeyFile: key, CAFile: ca}, wantMTLS: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, cli, err := BuildTLSConfigs(tc.opt)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if srv != nil || cli != nil {
					t.Fatalf("expected nil configs")
				}
				return
			}
			if srv == nil || cli == nil {
				t.Fatalf("expected configs, got srv=%v cli=%v", srv, cli)
			}
			if tc.wantMTLS {
				if srv.ClientCAs == nil || cli.RootCAs == nil {
					t.Fatalf("mTLS expected CA pools")
				}
			}
		})
	}
}

func TestDialTLSRoundtrip(t *testing.T) {
	cert, key, ca := writeSelfSignedTLS(t)

	srvCfg, cliCfg, err := BuildTLSConfigs(TLSOptions{CertFile: cert, KeyFile: key, CAFile: ca, ServerName: "127.0.0.1"})
	if err != nil {
		t.Fatalf("BuildTLSConfigs: %v", err)
	}

	secret := testSecret(0x77)
	impl := &handshakeImpl{secret: secret, peers: nil, viewVersion: 1, layoutVersion: 1}

	mux := drpcmux.New()
	if err := rpc.DRPCRegisterCluster(mux, impl); err != nil {
		t.Fatalf("register: %v", err)
	}
	srv := drpcserver.New(mux)

	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln := tls.NewListener(raw, srvCfg)
	defer ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = srv.Serve(ctx, ln)
	}()

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer dialCancel()

	conn, ack, err := Dial(dialCtx, ln.Addr().String(), "client", secret, cliCfg)
	if err != nil {
		t.Fatalf("Dial via TLS: %v", err)
	}
	defer conn.Close()

	if !ack.Accepted {
		t.Fatalf("handshake refused: %s", ack.Reason)
	}

	cancel()
	wg.Wait()
}
