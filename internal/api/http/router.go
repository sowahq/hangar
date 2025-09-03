package http

import (
	"time"

	"github.com/anhostfr/hangar/internal/api/http/handlers"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

func Router() *fiber.App {
	router := fiber.New(fiber.Config{
		BodyLimit:                    0, // we stream the request body so no limit
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true, // Disable pre-parsing of multipart form data since we handle it manually for file uploads
		IdleTimeout:                  3 * time.Minute,
		DisableStartupMessage:        true,
		Network:                      "tcp",
		ReadTimeout:                  10 * time.Second,
	})

	router.Get("/status", handlers.Status)

	// Admin API - Bucket management
	admin := router.Group("/admin")
	admin.Get("/buckets", handlers.ListBuckets)
	admin.Put("/buckets/:bucket", handlers.CreateBucket)
	admin.Get("/buckets/:bucket", handlers.GetBucket)
	admin.Delete("/buckets/:bucket", handlers.DeleteBucket)

	router.Get("/:bucket", handlers.ListObjects)
	router.Get("/:bucket/*", handlers.Download)
	router.Put("/:bucket/*", handlers.Upload)

	router.Hooks().OnListen(func(data fiber.ListenData) error {
		log.Info().Msgf("Started web server on %s:%s", data.Host, data.Port)
		return nil
	})

	return router
}
