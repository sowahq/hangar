package cluster

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/cockroachdb/pebble"

	"github.com/anhostfr/hangar/internal/database"
)

const (
	layoutKeyPrefix  = "cluster:layout:"
	layoutCurrentKey = "cluster:layout:current"
)

var (
	ErrLayoutNotFound  = errors.New("layout not found")
	ErrLayoutSignature = errors.New("layout signature mismatch")
	ErrLayoutStale     = errors.New("layout version stale")
)

type LayoutNode struct {
	ID       NodeID   `json:"id"`
	Addr     string   `json:"addr"`
	Zone     string   `json:"zone,omitempty"`
	Capacity int64    `json:"capacity,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Status   Status   `json:"status,omitempty"`
}

type Layout struct {
	Version uint64       `json:"version"`
	Nodes   []LayoutNode `json:"nodes"`
	HMAC    []byte       `json:"hmac,omitempty"`
}

func (l *Layout) normalize() {
	sort.Slice(l.Nodes, func(i, j int) bool { return l.Nodes[i].ID < l.Nodes[j].ID })
	for i := range l.Nodes {
		if len(l.Nodes[i].Tags) > 0 {
			tags := append([]string(nil), l.Nodes[i].Tags...)
			sort.Strings(tags)
			l.Nodes[i].Tags = tags
		}
	}
}

func (l *Layout) signingBytes() []byte {
	tmp := struct {
		Version uint64       `json:"v"`
		Nodes   []LayoutNode `json:"n"`
	}{Version: l.Version, Nodes: l.Nodes}
	b, _ := json.Marshal(tmp)
	return b
}

func (l *Layout) Sign(secret []byte) {
	l.normalize()
	mac := hmac.New(sha256.New, secret)
	mac.Write(l.signingBytes())
	l.HMAC = mac.Sum(nil)
}

func (l *Layout) Verify(secret []byte) error {
	if len(l.HMAC) == 0 {
		return ErrLayoutSignature
	}
	expect := hmac.New(sha256.New, secret)
	expect.Write(l.signingBytes())
	if !hmac.Equal(expect.Sum(nil), l.HMAC) {
		return ErrLayoutSignature
	}
	return nil
}

func layoutKey(v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	return append([]byte(layoutKeyPrefix), buf[:]...)
}

func GetLayout(v uint64) (*Layout, error) {
	db := database.LocalStore()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	data, err := db.Get(layoutKey(v))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil, ErrLayoutNotFound
		}
		return nil, err
	}
	var l Layout
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("unmarshal layout v%d: %w", v, err)
	}
	return &l, nil
}

func CurrentLayoutVersion() (uint64, error) {
	db := database.LocalStore()
	if db == nil {
		return 0, errors.New("database not initialized")
	}
	data, err := db.Get([]byte(layoutCurrentKey))
	if err != nil {
		if err == pebble.ErrNotFound {
			return 0, nil
		}
		return 0, err
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid current-layout value length %d", len(data))
	}
	return binary.BigEndian.Uint64(data), nil
}

func CurrentLayout() (*Layout, error) {
	v, err := CurrentLayoutVersion()
	if err != nil {
		return nil, err
	}
	if v == 0 {
		return nil, ErrLayoutNotFound
	}
	return GetLayout(v)
}

func ApplyLayout(l *Layout, secret []byte) error {
	if l == nil {
		return errors.New("nil layout")
	}
	if l.Version == 0 {
		return errors.New("layout version must be >= 1")
	}
	cur, err := CurrentLayoutVersion()
	if err != nil {
		return err
	}
	if l.Version <= cur {
		return ErrLayoutStale
	}
	l.Sign(secret)
	if err := l.Verify(secret); err != nil {
		return err
	}

	db := database.LocalStore()
	if db == nil {
		return errors.New("database not initialized")
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if err := db.Put(layoutKey(l.Version), data); err != nil {
		return err
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], l.Version)
	return db.Put([]byte(layoutCurrentKey), buf[:])
}

func UnmarshalLayout(raw []byte) (*Layout, error) {
	var l Layout
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (c *Cluster) ApplyLayout(l *Layout) error {
	if err := ApplyLayout(l, c.cfg.Secret); err != nil {
		return err
	}
	c.mu.Lock()
	c.layoutV = l.Version
	c.layout = l
	cb := c.layoutCB
	c.mu.Unlock()
	if cb != nil {
		cb()
	}
	return nil
}

func (c *Cluster) verifyLayout(l *Layout) error {
	if err := l.Verify(c.cfg.Secret); err == nil {
		return nil
	} else if !errors.Is(err, ErrLayoutSignature) {
		return err
	}
	if len(c.cfg.PreviousSecret) > 0 {
		return l.Verify(c.cfg.PreviousSecret)
	}
	return ErrLayoutSignature
}

func (c *Cluster) AdoptLayout(l *Layout) error {
	if l == nil {
		return errors.New("nil layout")
	}
	if err := c.verifyLayout(l); err != nil {
		return err
	}
	db := database.LocalStore()
	if db == nil {
		return errors.New("database not initialized")
	}
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	if err := db.PutSilent(layoutKey(l.Version), data); err != nil {
		return err
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], l.Version)
	if err := db.PutSilent([]byte(layoutCurrentKey), buf[:]); err != nil {
		return err
	}
	c.mu.Lock()
	c.layoutV = l.Version
	c.layout = l
	cb := c.layoutCB
	c.mu.Unlock()
	if cb != nil {
		cb()
	}
	return nil
}

func (c *Cluster) SetLayoutCallback(fn func()) {
	c.mu.Lock()
	c.layoutCB = fn
	c.mu.Unlock()
}

func (c *Cluster) RemoveNodeFromLayout(id NodeID) error {
	c.mu.RLock()
	cur := c.layout
	c.mu.RUnlock()
	if cur == nil {
		return errors.New("no layout applied")
	}

	out := &Layout{Version: cur.Version + 1}
	found := false
	for _, n := range cur.Nodes {
		if n.ID == id {
			found = true
			continue
		}
		nn := n
		nn.Tags = append([]string(nil), n.Tags...)
		out.Nodes = append(out.Nodes, nn)
	}
	if !found {
		return errors.New("node not in layout")
	}
	return c.ApplyLayout(out)
}

func (c *Cluster) DrainNodeInLayout(id NodeID) error {
	c.mu.RLock()
	cur := c.layout
	c.mu.RUnlock()
	if cur == nil {
		return errors.New("no layout applied")
	}

	out := &Layout{Version: cur.Version + 1}
	found := false
	for _, n := range cur.Nodes {
		nn := n
		nn.Tags = append([]string(nil), n.Tags...)
		if nn.ID == id {
			found = true
			nn.Status = StatusDraining
		}
		out.Nodes = append(out.Nodes, nn)
	}
	if !found {
		return errors.New("node not in layout")
	}
	return c.ApplyLayout(out)
}

func (c *Cluster) Layout() *Layout {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.layout
}

func (c *Cluster) LoadLayout() error {
	l, err := CurrentLayout()
	if err != nil {
		if errors.Is(err, ErrLayoutNotFound) {
			return nil
		}
		return err
	}
	if err := c.verifyLayout(l); err != nil {
		return err
	}
	c.mu.Lock()
	c.layoutV = l.Version
	c.layout = l
	cb := c.layoutCB
	c.mu.Unlock()
	if cb != nil {
		cb()
	}
	return nil
}
