package audit

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/urfave/cli/v2"
)

func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "tail",
			Usage: "Show the most recent audit events",
			Flags: []cli.Flag{
				common.ServerFlag(),
				&cli.IntFlag{
					Name:    "limit",
					Usage:   "Maximum number of events to return (max 1000)",
					Aliases: []string{"n"},
					Value:   100,
				},
			},
			Action: tailAudit,
		},
	}
}

func tailAudit(c *cli.Context) error {
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.TailAudit(c.Int("limit"))
	if err != nil {
		return fmt.Errorf("failed to read audit log: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	return enc.Encode(out)
}
