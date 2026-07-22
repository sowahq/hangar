package metrics

import (
	"time"

	"github.com/sowahq/hangar/internal/service/metrics"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
)

func Router() *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		IdleTimeout:           30 * time.Second,
		Network:               "tcp",
	})

	app.Get("/metrics", metrics.Handler())

	app.Get("/healthz", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	app.Hooks().OnListen(func(data fiber.ListenData) error {
		log.Info().Msgf("Started metrics server on %s:%s", data.Host, data.Port)
		return nil
	})

	return app
}
