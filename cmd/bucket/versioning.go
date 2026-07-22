package bucket

import (
	"fmt"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/urfave/cli/v2"
)

func versioningCommand() *cli.Command {
	return &cli.Command{
		Name:      "versioning",
		Usage:     "Enable or suspend bucket versioning",
		ArgsUsage: "<bucket-name>",
		Flags: []cli.Flag{
			common.ServerFlag(),
			&cli.StringFlag{
				Name:     "status",
				Usage:    "Versioning status (enabled or suspended)",
				Required: true,
			},
		},
		Action: setVersioning,
	}
}

func setVersioning(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}

	var enabled bool
	switch c.String("status") {
	case "enabled":
		enabled = true
	case "suspended":
		enabled = false
	default:
		return fmt.Errorf("status must be 'enabled' or 'suspended'")
	}

	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.UpdateBucketVersioning(c.Args().First(), enabled)
	if err != nil {
		return fmt.Errorf("failed to update versioning: %w", err)
	}

	printJSON("Versioning updated:", out)

	return nil
}
