package handlers

import (
	"crypto/subtle"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/sowahq/hangar/internal/api/http/response"
	"github.com/sowahq/hangar/internal/config"
	"github.com/sowahq/hangar/internal/database"
	"github.com/sowahq/hangar/internal/service/gc"
	"github.com/sowahq/hangar/pkg/sysinfo"
)

func statusDetailAllowed(c *fiber.Ctx) bool {
	token := config.AdminToken()
	if token == "" {
		return config.AllowUnauthenticatedAdmin()
	}

	raw := strings.TrimSpace(strings.TrimPrefix(c.Get("Authorization"), "Bearer "))
	return subtle.ConstantTimeCompare([]byte(raw), []byte(token)) == 1
}

type healthCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

func Status(c *fiber.Ctx) error {
	checks := make([]healthCheck, 0, 4)
	overall := "ok"

	dbOK := true
	db := database.LocalStore()
	if db == nil {
		dbOK = false
		checks = append(checks, healthCheck{Name: "db", OK: false, Detail: "not initialized"})
	} else {
		if _, err := db.Exist([]byte("__healthcheck__")); err != nil {
			dbOK = false
			checks = append(checks, healthCheck{Name: "db", OK: false, Detail: err.Error()})
		} else {
			checks = append(checks, healthCheck{Name: "db", OK: true})
		}
	}
	if !dbOK {
		overall = "degraded"
	}

	dataPath := config.DataPath()
	dataOK := true
	if _, err := os.Stat(dataPath); err != nil {
		dataOK = false
		overall = "degraded"
		checks = append(checks, healthCheck{Name: "data_dir", OK: false, Detail: err.Error()})
	} else {
		checks = append(checks, healthCheck{Name: "data_dir", OK: true})
	}

	diskFree := sysinfo.DiskFreeBytes(dataPath)
	if diskFree < 0 {
		checks = append(checks, healthCheck{Name: "disk_free", OK: false, Detail: "unsupported platform"})
	} else {
		checks = append(checks, healthCheck{Name: "disk_free", OK: true})
	}

	lastTick := gc.LastTick()
	gcStr := ""
	if !lastTick.IsZero() {
		gcStr = lastTick.UTC().Format(time.RFC3339)
	}
	checks = append(checks, healthCheck{Name: "gc", OK: true})

	if !statusDetailAllowed(c) {
		for i := range checks {
			checks[i].Detail = ""
		}
		return response.JSON(c, fiber.Map{
			"status":       overall,
			"db":           dbOK,
			"data_dir_ok":  dataOK,
			"gc_last_tick": gcStr,
			"checks":       checks,
		})
	}

	return response.JSON(c, fiber.Map{
		"status":          overall,
		"db":              dbOK,
		"data_dir_ok":     dataOK,
		"disk_free_bytes": diskFree,
		"gc_last_tick":    gcStr,
		"checks":          checks,
		"go_version":      runtime.Version(),
	})
}
