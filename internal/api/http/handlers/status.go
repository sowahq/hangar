package handlers

import (
	"os"
	"runtime"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/anhostfr/hangar/internal/api/http/response"
	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	"github.com/anhostfr/hangar/internal/service/gc"
	"github.com/anhostfr/hangar/pkg/sysinfo"
)

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
