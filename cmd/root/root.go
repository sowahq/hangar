package root

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/anhostfr/hangar/cmd/bucket"
	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/api/http"
	"github.com/phuslu/log"
	"github.com/urfave/cli/v2"
)

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

					// TODO: make a main file or idk its trash rn

					if err := os.MkdirAll(config.ServerConfig().DataDirectory, 0755); err != nil {
						log.Error().Err(err).Msg("Failed to create data directory.")
					}

					if err := os.MkdirAll(config.ChunksPath(), 0755); err != nil {
						log.Error().Err(err).Msg("Failed to create chunks directory.")
					}

					log.Debug().Msgf("Created data directory: %s", config.ServerConfig().DataDirectory)

					router := http.Router()
					go router.Listen(config.ServerConfig().API.BindAddr)

					osSignal := make(chan os.Signal, 1)
					signal.Notify(osSignal, os.Interrupt, syscall.SIGTERM)

					<-osSignal

					log.Info().Msg("Shutting down Hangar...")

					if err := router.Shutdown(); err != nil {
						log.Error().Err(err).Msg("Failed to shutdown server.")
					}

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
