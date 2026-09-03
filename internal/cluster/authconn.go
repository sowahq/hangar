package cluster

import (
	"net"
	"sync"

	"github.com/sowahq/hangar/internal/api/rpc"
)

type authListener struct {
	net.Listener
	auth *rpc.ConnAuth
}

func newAuthListener(ln net.Listener, auth *rpc.ConnAuth) net.Listener {
	return &authListener{Listener: ln, auth: auth}
}

func (l *authListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	return &authConn{Conn: c, auth: l.auth}, nil
}

type authConn struct {
	net.Conn
	auth *rpc.ConnAuth
	once sync.Once
}

func (c *authConn) Close() error {
	c.once.Do(func() {
		c.auth.Forget(c)
	})

	return c.Conn.Close()
}
