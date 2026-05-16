package admin

import (
	"errors"
	"strconv"

	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/service/audit"
	"github.com/gofiber/fiber/v2"
)

func TailAudit(c *fiber.Ctx) error {
	limit := 100

	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return response.Error(c, fiber.StatusBadRequest, "Invalid limit")
		}
		if n > 1000 {
			n = 1000
		}
		limit = n
	}

	events, err := audit.Tail(limit)
	if err != nil {
		if errors.Is(err, audit.ErrDisabled) {
			return response.Error(c, fiber.StatusServiceUnavailable, "Audit log disabled")
		}
		return response.ErrorWithLog(c, fiber.StatusInternalServerError, "Failed to read audit log", err, "audit tail")
	}

	return response.JSON(c, fiber.Map{"events": events, "count": len(events)})
}
