package root

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/anhostfr/hangar/cmd/backup"
	"github.com/anhostfr/hangar/cmd/bucket"
	"github.com/anhostfr/hangar/cmd/s3keys"
	"github.com/anhostfr/hangar/internal/api/http"
	"github.com/anhostfr/hangar/internal/api/s3"
	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	gcService "github.com/anhostfr/hangar/internal/service/gc"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/gofiber/fiber/v2"
	"github.com/phuslu/log"
	"github.com/urfave/cli/v2"
)

const shutdownTimeout = 30 * time.Second

func Execute() {
	app := &cli.App{
		Name:        "hangar",
		Description: "Object Storage CLI",
		Commands: []*cli.Command{
			{
				Name:        "bucket",
				Usage:       "Manage buckets",
				Subcommands: bucket.Commands(),
			},
			{
				Name:        "s3keys",
				Usage:       "Manage S3 access keys",
				Subcommands: s3keys.Commands(),
			},
			{
				Name:        "backup",
				Usage:       "Create and restore data backups",
				Subcommands: backup.Commands(),
			},
			{
				Name:  "server",
				Usage: "Start the object storage server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "config",
						Usage:   "Path to the configuration file",
						Aliases: []string{"c"},
						Value:   "config.toml",
					},
				},
				Action: func(c *cli.Context) error {
					configPath := c.String("config")

					log.Info().Msgf("Starting Hangar server with config file: %s", configPath)

					if err := config.LoadServerConfig(configPath); err != nil {
						log.Error().Err(err).Msg("Failed to load configuration.")
						return err
					}

					if err := os.MkdirAll(config.ServerConfig().DataDirectory, 0755); err != nil {
						log.Error().Err(err).Msg("Failed to create data directory.")
					}

					if err := os.MkdirAll(config.ChunksPath(), 0755); err != nil {
						log.Error().Err(err).Msg("Failed to create chunks directory.")
					}

					log.Debug().Msgf("Created data directory: %s", config.ServerConfig().DataDirectory)

					if err := storage.BootstrapChunkRefs(); err != nil {
						log.Error().Err(err).Msg("Failed to bootstrap chunkref index.")
						return err
					}

					httpRouter := http.Router()
					httpErr := make(chan error, 1)
					go func() {
						httpErr <- httpRouter.Listen(config.ServerConfig().API.BindAddr)
					}()

					var s3Router *fiber.App
					s3Err := make(chan error, 1)
					if config.S3Enabled() {
						s3Router = s3.Router()
						go func() {
							s3Err <- s3Router.Listen(config.S3BindAddr())
						}()
					}

					ctx, cancel := context.WithCancel(context.Background())
					gcDone := make(chan struct{})
					go gcService.StartScheduledGC(ctx, gcDone)

					osSignal := make(chan os.Signal, 1)
					signal.Notify(osSignal, os.Interrupt, syscall.SIGTERM)

					select {
					case sig := <-osSignal:
						log.Info().Str("signal", sig.String()).Msg("Shutdown signal received")
					case err := <-httpErr:
						log.Error().Err(err).Msg("HTTP server exited unexpectedly")
					case err := <-s3Err:
						log.Error().Err(err).Msg("S3 server exited unexpectedly")
					}

					log.Info().Dur("timeout", shutdownTimeout).Msg("Shutting down Hangar...")

					if err := httpRouter.ShutdownWithTimeout(shutdownTimeout); err != nil {
						log.Error().Err(err).Msg("HTTP server shutdown error")
					}
					if s3Router != nil {
						if err := s3Router.ShutdownWithTimeout(shutdownTimeout); err != nil {
							log.Error().Err(err).Msg("S3 server shutdown error")
						}
					}

					cancel()
					select {
					case <-gcDone:
					case <-time.After(shutdownTimeout):
						log.Warn().Msg("GC goroutine did not exit within timeout")
					}

					if err := database.Close(); err != nil {
						log.Error().Err(err).Msg("Failed to close database")
					}

					log.Info().Msg("Hangar stopped")
					return nil
				},
			},
		},
	}
	err := app.Run(os.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nERROR: %v\n", err)
		os.Exit(1)
	}
}
