package http

import (
	"time"

	"github.com/anhostfr/hangar/internal/http/handlers"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

func Router() *fiber.App {
	router := fiber.New(fiber.Config{
		BodyLimit:                    0, // we stream the request body so no limit
		StreamRequestBody:            true,
		DisablePreParseMultipartForm: true, // Disable pre-parsing of multipart form data since we handle it manually for file uploads
		IdleTimeout:                  3 * time.Minute,
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			log.Error().Err(err).Msgf("Error in %s", ctx.OriginalURL())

			// TODO: Better error handling etc.
			if e, ok := err.(*fiber.Error); ok {
				return ctx.SendStatus(e.Code)
			}

			return ctx.Status(fiber.StatusInternalServerError).SendString(err.Error())
		},
		DisableStartupMessage: true,
		Network:               "tcp",
	})

	router.Get("/status", handlers.Status)
	
	// Bucket routes
	router.Post("/buckets", handlers.CreateBucket)
	router.Get("/buckets", handlers.ListBuckets)
	router.Get("/buckets/:name", handlers.GetBucket)
	router.Delete("/buckets/:name", handlers.DeleteBucket)
	
	// Object routes (bucket-scoped only)
	router.Get("/buckets/:bucket/objects/*", handlers.ListObjects)
	router.Get("/buckets/:bucket/download/*", handlers.Download)
	router.Post("/buckets/:bucket/upload/*", handlers.Upload)

	router.Hooks().OnListen(func(data fiber.ListenData) error {
		log.Info().Msgf("Started web server on %s:%s", data.Host, data.Port)
		return nil
	})

	return router
}
