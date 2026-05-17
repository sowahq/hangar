package cluster

import (
	"sort"
)

func (c *Cluster) layoutNodes() []Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.layout != nil && len(c.layout.Nodes) > 0 {
		out := make([]Node, 0, len(c.layout.Nodes))
		for _, n := range c.layout.Nodes {
			w := float64(n.Capacity)
			if w <= 0 {
				w = 1.0
			}
			out = append(out, Node{ID: string(n.ID), Weight: w})
		}
		return out
	}

	out := make([]Node, 0, len(c.view.Nodes))
	for id := range c.view.Nodes {
		out = append(out, Node{ID: string(id), Weight: 1.0})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (c *Cluster) aliveLayoutNodes() []Node {
	c.mu.RLock()
	defer c.mu.RUnlock()

	alive := map[NodeID]struct{}{}
	for id, ns := range c.view.Nodes {
		if ns.Status == StatusActive {
			alive[id] = struct{}{}
		}
	}

	var src []LayoutNode
	if c.layout != nil && len(c.layout.Nodes) > 0 {
		src = c.layout.Nodes
	} else {
		src = make([]LayoutNode, 0, len(c.view.Nodes))
		for id := range c.view.Nodes {
			src = append(src, LayoutNode{ID: id})
		}
		sort.Slice(src, func(i, j int) bool { return src[i].ID < src[j].ID })
	}

	out := make([]Node, 0, len(src))
	for _, n := range src {
		if _, ok := alive[n.ID]; !ok {
			continue
		}
		if n.Status == StatusDraining {
			continue
		}
		w := float64(n.Capacity)
		if w <= 0 {
			w = 1.0
		}
		out = append(out, Node{ID: string(n.ID), Weight: w})
	}
	return out
}

func (c *Cluster) BucketPrimary(bucket string) NodeID {
	nodes := c.aliveLayoutNodes()
	if len(nodes) == 0 {
		return ""
	}
	ranked := RankNodes("bucket:"+bucket, nodes)
	return NodeID(ranked[0].ID)
}

func (c *Cluster) ObjectShardPrimary(bucket, key string) NodeID {
	nodes := c.aliveLayoutNodes()
	if len(nodes) == 0 {
		return ""
	}
	ranked := RankNodes(bucket+"/"+key, nodes)
	return NodeID(ranked[0].ID)
}

func (c *Cluster) ObjectShardReplicas(bucket, key string, count int) []NodeID {
	if count <= 0 {
		return nil
	}
	nodes := c.aliveLayoutNodes()
	if len(nodes) == 0 {
		return nil
	}
	top := TopN(bucket+"/"+key, nodes, count)
	out := make([]NodeID, len(top))
	for i, n := range top {
		out[i] = NodeID(n.ID)
	}
	return out
}

func (c *Cluster) ChunkOwners(hash string, count int) []NodeID {
	if count <= 0 {
		return nil
	}
	nodes := c.aliveLayoutNodes()
	if len(nodes) == 0 {
		return nil
	}
	top := TopN("chunk:"+hash, nodes, count)
	out := make([]NodeID, len(top))
	for i, n := range top {
		out[i] = NodeID(n.ID)
	}
	return out
}

func (c *Cluster) ChunkOwnersStable(hash string, count int) []NodeID {
	if count <= 0 {
		return nil
	}
	nodes := c.layoutNodes()
	if len(nodes) == 0 {
		return nil
	}
	top := TopN("chunk:"+hash, nodes, count)
	out := make([]NodeID, len(top))
	for i, n := range top {
		out[i] = NodeID(n.ID)
	}
	return out
}

func (c *Cluster) IsLocal(id NodeID) bool {
	return id == c.cfg.NodeID
}

func (c *Cluster) NodeAddr(id NodeID) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.layout != nil {
		for _, n := range c.layout.Nodes {
			if n.ID == id {
				return n.Addr
			}
		}
	}
	if ns, ok := c.view.Nodes[id]; ok {
		return ns.Addr
	}
	return ""
}
