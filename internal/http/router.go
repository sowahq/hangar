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
	router.Get("/objects/*", handlers.ListObjects)
	router.Get("/download/*", handlers.Download)
	router.Post("/upload/*", handlers.Upload)

	router.Hooks().OnListen(func(data fiber.ListenData) error {
		log.Info().Msgf("Started web server on %s:%s", data.Host, data.Port)
		return nil
	})

	return router
}
