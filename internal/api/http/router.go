package http

import (
	"time"

	"github.com/anhostfr/hangar/internal/api/http/admin"
	"github.com/anhostfr/hangar/internal/api/http/handlers"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

func Router() *fiber.App {
	router := fiber.New(fiber.Config{
		BodyLimit:                    0,
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true,
		IdleTimeout:                  3 * time.Minute,
		DisableStartupMessage:        true,
		Network:                      "tcp",
	})

	router.Get("/status", handlers.Status)

	adminGroup := router.Group("/admin")
	adminGroup.Get("/buckets", admin.ListBuckets)
	adminGroup.Put("/buckets/:bucket", admin.CreateBucket)
	adminGroup.Get("/buckets/:bucket", admin.GetBucket)
	adminGroup.Delete("/buckets/:bucket", admin.DeleteBucket)

	router.Get("/:bucket", handlers.ListObjects)
	router.Get("/:bucket/*", handlers.Download)
	router.Put("/:bucket/*", handlers.Upload)
	router.Delete("/:bucket/*", handlers.Delete)

	router.Hooks().OnListen(func(data fiber.ListenData) error {
		log.Info().Msgf("Started web server on %s:%s", data.Host, data.Port)
		return nil
	})

	return router
}
