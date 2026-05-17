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

func ClusterLayoutGet(c *fiber.Ctx) error {
	cl := cluster.Global()
	if cl == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "cluster mode disabled")
	}
	l := cl.Layout()
	if l == nil {
		return response.Error(c, fiber.StatusNotFound, "no layout applied")
	}
	return response.JSON(c, l)
}

func ClusterLayoutApply(c *fiber.Ctx) error {
	cl := cluster.Global()
	if cl == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "cluster mode disabled")
	}
	var l cluster.Layout
	if err := c.BodyParser(&l); err != nil {
		return response.Error(c, fiber.StatusBadRequest, "invalid layout json: "+err.Error())
	}
	if err := cl.ApplyLayout(&l); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, fiber.Map{"version": l.Version, "ok": true})
}

func ClusterNodeRemove(c *fiber.Ctx) error {
	cl := cluster.Global()
	if cl == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "cluster mode disabled")
	}
	id := cluster.NodeID(c.Params("id"))
	if id == "" {
		return response.Error(c, fiber.StatusBadRequest, "id required")
	}
	if id == cl.Self() {
		return response.Error(c, fiber.StatusBadRequest, "cannot remove self")
	}
	if err := cl.RemoveNodeFromLayout(id); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, fiber.Map{"removed": string(id), "layout_version": cl.LayoutVersion()})
}

func ClusterAntiEntropyRun(c *fiber.Ctx) error {
	rt := cluster.GlobalRuntime()
	if rt == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "cluster mode disabled")
	}
	stats, err := rt.RunAntiEntropy(c.UserContext())
	if err != nil {
		return response.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return response.JSON(c, fiber.Map{
		"scanned":  stats.Scanned,
		"pulled":   stats.Pulled,
		"deleted":  stats.Deleted,
		"errors":   stats.Errors,
		"duration_ms": stats.EndedAt.Sub(stats.StartedAt).Milliseconds(),
	})
}

func ClusterNodeDrain(c *fiber.Ctx) error {
	cl := cluster.Global()
	if cl == nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "cluster mode disabled")
	}
	id := cluster.NodeID(c.Params("id"))
	if id == "" {
		return response.Error(c, fiber.StatusBadRequest, "id required")
	}
	if err := cl.DrainNodeInLayout(id); err != nil {
		return response.Error(c, fiber.StatusBadRequest, err.Error())
	}
	return response.JSON(c, fiber.Map{"draining": string(id), "layout_version": cl.LayoutVersion()})
}
