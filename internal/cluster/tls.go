package cluster

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

type TLSOptions struct {
	CertFile   string
	KeyFile    string
	CAFile     string
	ServerName string
}

func (o TLSOptions) Enabled() bool {
	return o.CertFile != "" || o.KeyFile != "" || o.CAFile != ""
}

func BuildTLSConfigs(o TLSOptions) (server, client *tls.Config, err error) {
	if !o.Enabled() {
		return nil, nil, nil
	}
	if o.CertFile == "" || o.KeyFile == "" {
		return nil, nil, errors.New("cluster: tls_cert and tls_key are both required when TLS is configured")
	}
	if o.CAFile == "" {
		return nil, nil, errors.New("cluster: tls_ca is required when TLS is configured (mutual authentication)")
	}

	cert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
	if err != nil {
		return nil, nil, fmt.Errorf("load cert/key: %w", err)
	}

	raw, rerr := os.ReadFile(o.CAFile)
	if rerr != nil {
		return nil, nil, fmt.Errorf("read ca: %w", rerr)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(raw) {
		return nil, nil, errors.New("cluster: tls_ca contains no valid PEM certificates")
	}

	server = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}
	client = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		ServerName:   o.ServerName,
		RootCAs:      caPool,
	}

	return server, client, nil
}
