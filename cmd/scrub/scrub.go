package scrub

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anhostfr/hangar/internal/config"
	"github.com/anhostfr/hangar/internal/database"
	scrubsvc "github.com/anhostfr/hangar/internal/service/scrub"
	"github.com/urfave/cli/v2"
)

func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "run",
			Usage: "Run integrity scrub: re-hash chunks, quarantine corrupted, report dangling refs (server must be stopped)",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "config",
					Aliases: []string{"c"},
					Usage:   "Path to the configuration file",
					Value:   "config.toml",
				},
				&cli.BoolFlag{
					Name:  "dry-run",
					Usage: "Report findings without quarantining corrupted chunks",
				},
				&cli.Int64Flag{
					Name:  "rate",
					Usage: "Max bytes/sec to scan (0 = unlimited)",
					Value: 0,
				},
			},
			Action: runScrub,
		},
	}
}

func runScrub(c *cli.Context) error {
	if err := config.LoadServerConfig(c.String("config")); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	defer func() {
		_ = database.Close()
	}()

	stats, err := scrubsvc.Run(scrubsvc.Opts{
		DryRun:          c.Bool("dry-run"),
		RateBytesPerSec: c.Int64("rate"),
		Context:         context.Background(),
	})
	if err != nil {
		return fmt.Errorf("scrub failed: %w", err)
	}

	data, _ := json.MarshalIndent(stats, "", "  ")
	fmt.Printf("Scrub report:\n%s\n", data)
	return nil
}
