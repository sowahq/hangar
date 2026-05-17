package admin

import (
	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/cluster"
	"github.com/gofiber/fiber/v2"
)

type clusterNodeOut struct {
	ID         string   `json:"id"`
	Addr       string   `json:"addr"`
	Zone       string   `json:"zone,omitempty"`
	Capacity   int64    `json:"capacity,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Generation uint64   `json:"generation"`
	LastSeenMS int64    `json:"last_seen_ms"`
	Status     string   `json:"status"`
}

func ClusterStatus(c *fiber.Ctx) error {
	cl := cluster.Global()
	if cl == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "cluster mode disabled")
	}

	v := cl.View()

	nodes := make([]clusterNodeOut, 0, len(v.Nodes))
	for _, n := range v.Nodes {
		nodes = append(nodes, clusterNodeOut{
			ID:         string(n.ID),
			Addr:       n.Addr,
			Zone:       n.Zone,
			Capacity:   n.Capacity,
			Tags:       n.Tags,
			Generation: n.Generation,
			LastSeenMS: n.LastSeen.UnixMilli(),
			Status:     n.Status.String(),
		})
	}

	return response.JSON(c, fiber.Map{
		"self":           string(cl.Self()),
		"view_version":   v.Version,
		"layout_version": cl.LayoutVersion(),
		"heartbeat_ms":   int(cl.HeartbeatInterval().Milliseconds()),
		"nodes":          nodes,
	})
}
