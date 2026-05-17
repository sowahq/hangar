package cluster

import (
	"sort"
	"sync"
	"time"

	"github.com/anhostfr/hangar/internal/api/rpc"
)

type NodeID string

type Status uint8

const (
	StatusUnknown Status = iota
	StatusActive
	StatusSuspect
	StatusDown
	StatusDraining
)

func (s Status) String() string {
	switch s {
	case StatusActive:
		return "active"
	case StatusSuspect:
		return "suspect"
	case StatusDown:
		return "down"
	case StatusDraining:
		return "draining"
	default:
		return "unknown"
	}
}

type NodeState struct {
	ID         NodeID
	Addr       string
	Zone       string
	Capacity   int64
	Tags       []string
	Generation uint64
	LastSeen   time.Time
	Status     Status
}

type View struct {
	Version uint64
	Nodes   map[NodeID]NodeState
}

func NewView() View {
	return View{Version: 0, Nodes: map[NodeID]NodeState{}}
}

func (v View) Clone() View {
	out := View{Version: v.Version, Nodes: make(map[NodeID]NodeState, len(v.Nodes))}
	for id, n := range v.Nodes {
		if n.Tags != nil {
			tags := make([]string, len(n.Tags))
			copy(tags, n.Tags)
			n.Tags = tags
		}
		out.Nodes[id] = n
	}
	return out
}

func (v View) Alive() []NodeState {
	out := make([]NodeState, 0, len(v.Nodes))
	for _, n := range v.Nodes {
		if n.Status == StatusActive {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (v View) AliveIDs() []NodeID {
	alive := v.Alive()
	out := make([]NodeID, len(alive))
	for i, n := range alive {
		out[i] = n.ID
	}
	return out
}

type ViewDiff struct {
	Added   []NodeID
	Removed []NodeID
	Changed []NodeID
}

func (d ViewDiff) Empty() bool {
	return len(d.Added) == 0 && len(d.Removed) == 0 && len(d.Changed) == 0
}

func DiffViews(old, new View) ViewDiff {
	var d ViewDiff

	for id, ns := range new.Nodes {
		prev, ok := old.Nodes[id]
		if !ok {
			d.Added = append(d.Added, id)
			continue
		}
		if nodeStateChanged(prev, ns) {
			d.Changed = append(d.Changed, id)
		}
	}

	for id := range old.Nodes {
		if _, ok := new.Nodes[id]; !ok {
			d.Removed = append(d.Removed, id)
		}
	}

	sort.Slice(d.Added, func(i, j int) bool { return d.Added[i] < d.Added[j] })
	sort.Slice(d.Removed, func(i, j int) bool { return d.Removed[i] < d.Removed[j] })
	sort.Slice(d.Changed, func(i, j int) bool { return d.Changed[i] < d.Changed[j] })

	return d
}

func nodeStateChanged(a, b NodeState) bool {
	if a.Status != b.Status || a.Generation != b.Generation {
		return true
	}
	if a.Addr != b.Addr || a.Zone != b.Zone || a.Capacity != b.Capacity {
		return true
	}
	if len(a.Tags) != len(b.Tags) {
		return true
	}
	for i := range a.Tags {
		if a.Tags[i] != b.Tags[i] {
			return true
		}
	}
	return false
}

func (v *View) Apply(next View) bool {
	if next.Version <= v.Version {
		return false
	}
	v.Version = next.Version
	v.Nodes = next.Clone().Nodes
	return true
}

func (v *View) Upsert(ns NodeState) bool {
	prev, ok := v.Nodes[ns.ID]
	if ok && !nodeStateChanged(prev, ns) {
		v.Nodes[ns.ID] = ns
		return false
	}
	if v.Nodes == nil {
		v.Nodes = map[NodeID]NodeState{}
	}
	v.Nodes[ns.ID] = ns
	v.Version++
	return true
}

func (v *View) Remove(id NodeID) bool {
	if _, ok := v.Nodes[id]; !ok {
		return false
	}
	delete(v.Nodes, id)
	v.Version++
	return true
}

type Config struct {
	NodeID      NodeID
	Listen      string
	Peers       map[NodeID]string
	Secret      []byte
	HeartbeatMS int
	Generation  uint64

	NowFn func() time.Time
}

type Cluster struct {
	cfg Config

	knownPeers map[string]struct{}

	mu      sync.RWMutex
	view    View
	layoutV uint64
	layout  *Layout
}

func New(cfg Config) *Cluster {
	if cfg.HeartbeatMS <= 0 {
		cfg.HeartbeatMS = 500
	}
	if cfg.NowFn == nil {
		cfg.NowFn = time.Now
	}

	known := map[string]struct{}{string(cfg.NodeID): {}}
	for id := range cfg.Peers {
		known[string(id)] = struct{}{}
	}

	c := &Cluster{
		cfg:        cfg,
		knownPeers: known,
		view:       NewView(),
	}

	now := cfg.NowFn()
	c.view.Upsert(NodeState{
		ID:         cfg.NodeID,
		Addr:       cfg.Listen,
		Generation: cfg.Generation,
		Status:     StatusActive,
		LastSeen:   now,
	})
	for id, addr := range cfg.Peers {
		c.view.Upsert(NodeState{ID: id, Addr: addr, Status: StatusUnknown})
	}

	return c
}

func (c *Cluster) Self() NodeID { return c.cfg.NodeID }

func (c *Cluster) Secret() []byte { return c.cfg.Secret }

func (c *Cluster) HeartbeatInterval() time.Duration {
	return time.Duration(c.cfg.HeartbeatMS) * time.Millisecond
}

func (c *Cluster) View() View {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.view.Clone()
}

func (c *Cluster) ViewVersion() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.view.Version
}

func (c *Cluster) LayoutVersion() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.layoutV
}

func (c *Cluster) NodeStatus(id NodeID) Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if ns, ok := c.view.Nodes[id]; ok {
		return ns.Status
	}
	return StatusUnknown
}

func (c *Cluster) VerifyHello(h *rpc.Hello, now time.Time) error {
	return VerifyHello(h, c.cfg.Secret, c.knownPeers, now)
}

func (c *Cluster) BuildHeartbeat() *rpc.Heartbeat {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &rpc.Heartbeat{
		NodeId:        string(c.cfg.NodeID),
		Generation:    c.cfg.Generation,
		ViewVersion:   c.view.Version,
		LayoutVersion: c.layoutV,
		Ts:            c.cfg.NowFn().UnixMilli(),
	}
}

func (c *Cluster) OnHeartbeat(h *rpc.Heartbeat) {
	if h == nil || h.NodeId == "" {
		return
	}

	id := NodeID(h.NodeId)
	if _, known := c.knownPeers[h.NodeId]; !known {
		return
	}
	if id == c.cfg.NodeID {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	state, ok := c.view.Nodes[id]
	if !ok {
		state = NodeState{ID: id}
	}
	state.LastSeen = c.cfg.NowFn()
	state.Status = StatusActive
	if h.Generation > state.Generation {
		state.Generation = h.Generation
	}
	c.view.Upsert(state)
}

func (c *Cluster) markStale(now time.Time) {
	deadline := now.Add(-3 * c.HeartbeatInterval())

	c.mu.Lock()
	defer c.mu.Unlock()

	for id, ns := range c.view.Nodes {
		if id == c.cfg.NodeID {
			continue
		}
		if ns.Status == StatusActive && ns.LastSeen.Before(deadline) {
			ns.Status = StatusDown
			c.view.Upsert(ns)
		}
	}
}
