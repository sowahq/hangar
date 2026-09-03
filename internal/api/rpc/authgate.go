package rpc

import (
	"errors"
	"sync"

	"storj.io/drpc"
	"storj.io/drpc/drpcctx"
	"storj.io/drpc/drpcerr"
)

const handshakeRPC = "/hangar.cluster.v1.Cluster/Handshake"

const codeUnauthenticated = 16

var errUnauthenticated = drpcerr.WithCode(errors.New("cluster: rpc requires a completed handshake"), codeUnauthenticated)

type ConnAuth struct {
	mu     sync.Mutex
	authed map[drpc.Transport]struct{}
}

func NewConnAuth() *ConnAuth {
	return &ConnAuth{authed: make(map[drpc.Transport]struct{})}
}

func (a *ConnAuth) Mark(tr drpc.Transport) {
	if a == nil || tr == nil {
		return
	}

	a.mu.Lock()
	a.authed[tr] = struct{}{}
	a.mu.Unlock()
}

func (a *ConnAuth) Forget(tr drpc.Transport) {
	if a == nil || tr == nil {
		return
	}

	a.mu.Lock()
	delete(a.authed, tr)
	a.mu.Unlock()
}

func (a *ConnAuth) authorized(tr drpc.Transport) bool {
	if a == nil || tr == nil {
		return false
	}

	a.mu.Lock()
	_, ok := a.authed[tr]
	a.mu.Unlock()
	return ok
}

type AuthGate struct {
	inner drpc.Handler
	auth  *ConnAuth
}

func NewAuthGate(inner drpc.Handler, auth *ConnAuth) *AuthGate {
	return &AuthGate{inner: inner, auth: auth}
}

func (g *AuthGate) HandleRPC(stream drpc.Stream, rpc string) error {
	if rpc == handshakeRPC {
		return g.inner.HandleRPC(stream, rpc)
	}

	tr, ok := drpcctx.Transport(stream.Context())
	if !ok || !g.auth.authorized(tr) {
		return errUnauthenticated
	}

	return g.inner.HandleRPC(stream, rpc)
}

var _ drpc.Handler = (*AuthGate)(nil)
