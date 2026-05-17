package cluster

import (
	"context"
	"errors"
	"sync"
	"time"

	"storj.io/drpc/drpcconn"

	"github.com/anhostfr/hangar/internal/api/rpc"
)

const peerDialTimeout = 3 * time.Second

type ConnPool struct {
	self   NodeID
	secret []byte
	resolv func(NodeID) string

	mu    sync.Mutex
	conns map[NodeID]*drpcconn.Conn
}

func NewConnPool(self NodeID, secret []byte, resolv func(NodeID) string) *ConnPool {
	return &ConnPool{
		self:   self,
		secret: secret,
		resolv: resolv,
		conns:  map[NodeID]*drpcconn.Conn{},
	}
}

func (p *ConnPool) Get(ctx context.Context, id NodeID) (*drpcconn.Conn, error) {
	if id == p.self {
		return nil, errors.New("conn pool: refusing to dial self")
	}

	p.mu.Lock()
	if c, ok := p.conns[id]; ok {
		select {
		case <-c.Closed():
			delete(p.conns, id)
		default:
			p.mu.Unlock()
			return c, nil
		}
	}
	p.mu.Unlock()

	addr := p.resolv(id)
	if addr == "" {
		return nil, ErrPeerUnavailable
	}

	dialCtx, cancel := context.WithTimeout(ctx, peerDialTimeout)
	defer cancel()

	conn, _, err := Dial(dialCtx, addr, string(p.self), p.secret)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	if existing, ok := p.conns[id]; ok {
		p.mu.Unlock()
		_ = conn.Close()
		return existing, nil
	}
	p.conns[id] = conn
	p.mu.Unlock()
	return conn, nil
}

func (p *ConnPool) Client(ctx context.Context, id NodeID) (rpc.DRPCClusterClient, error) {
	conn, err := p.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return rpc.NewDRPCClusterClient(conn), nil
}

func (p *ConnPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, c := range p.conns {
		_ = c.Close()
		delete(p.conns, id)
	}
}

func (p *ConnPool) Drop(id NodeID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.conns[id]; ok {
		_ = c.Close()
		delete(p.conns, id)
	}
}
