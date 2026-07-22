package cluster

import (
	"encoding/json"
	"sync"

	"github.com/sowahq/hangar/internal/api/rpc"
)

var joinMu sync.Mutex

type joinHandler struct {
	rt *Runtime
}

func (h *joinHandler) Join(req *rpc.JoinRequest) (*rpc.JoinResponse, error) {
	if h == nil || h.rt == nil || h.rt.Cluster == nil {
		return &rpc.JoinResponse{Accepted: false, Error: "cluster not ready"}, nil
	}
	if req.Id == "" || req.Addr == "" {
		return &rpc.JoinResponse{Accepted: false, Error: "id and addr required"}, nil
	}

	joinMu.Lock()
	defer joinMu.Unlock()

	cur := h.rt.Cluster.Layout()
	if cur == nil {
		l := &Layout{
			Version: 1,
			Nodes: []LayoutNode{
				h.rt.selfLayoutNode(),
				{ID: NodeID(req.Id), Addr: req.Addr, Zone: req.Zone, Capacity: req.Capacity, Tags: append([]string(nil), req.Tags...)},
			},
		}
		if err := h.rt.Cluster.ApplyLayout(l); err != nil {
			return &rpc.JoinResponse{Accepted: false, Error: err.Error()}, nil
		}
		signed, _ := json.Marshal(l)
		return &rpc.JoinResponse{Accepted: true, SignedLayout: signed}, nil
	}

	for _, n := range cur.Nodes {
		if n.ID == NodeID(req.Id) {
			if n.Addr == req.Addr {
				signed, _ := json.Marshal(cur)
				return &rpc.JoinResponse{Accepted: true, SignedLayout: signed}, nil
			}
			updated := cloneLayout(cur)
			for i := range updated.Nodes {
				if updated.Nodes[i].ID == NodeID(req.Id) {
					updated.Nodes[i].Addr = req.Addr
					updated.Nodes[i].Zone = req.Zone
					updated.Nodes[i].Capacity = req.Capacity
					updated.Nodes[i].Tags = append([]string(nil), req.Tags...)
				}
			}
			updated.Version = cur.Version + 1
			if err := h.rt.Cluster.ApplyLayout(updated); err != nil {
				return &rpc.JoinResponse{Accepted: false, Error: err.Error()}, nil
			}
			signed, _ := json.Marshal(updated)
			return &rpc.JoinResponse{Accepted: true, SignedLayout: signed}, nil
		}
	}

	updated := cloneLayout(cur)
	updated.Version = cur.Version + 1
	updated.Nodes = append(updated.Nodes, LayoutNode{
		ID:       NodeID(req.Id),
		Addr:     req.Addr,
		Zone:     req.Zone,
		Capacity: req.Capacity,
		Tags:     append([]string(nil), req.Tags...),
	})
	if err := h.rt.Cluster.ApplyLayout(updated); err != nil {
		return &rpc.JoinResponse{Accepted: false, Error: err.Error()}, nil
	}
	signed, _ := json.Marshal(updated)
	return &rpc.JoinResponse{Accepted: true, SignedLayout: signed}, nil
}

func cloneLayout(l *Layout) *Layout {
	out := &Layout{Version: l.Version}
	out.Nodes = make([]LayoutNode, len(l.Nodes))
	for i, n := range l.Nodes {
		nn := n
		nn.Tags = append([]string(nil), n.Tags...)
		out.Nodes[i] = nn
	}
	return out
}
