package bucket

import (
	"fmt"

	"github.com/anhostfr/hangar/internal/client"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/urfave/cli/v2"
)

func objectLockCommand() *cli.Command {
	return &cli.Command{
		Name:  "object-lock",
		Usage: "Manage bucket object lock configuration",
		Subcommands: []*cli.Command{
			{
				Name:      "enable",
				Usage:     "Enable object lock on a bucket (requires versioning)",
				ArgsUsage: "<bucket-name>",
				Flags: []cli.Flag{
					serverFlag(),
					&cli.StringFlag{Name: "default-mode", Usage: "Default retention mode (GOVERNANCE or COMPLIANCE)"},
					&cli.IntFlag{Name: "default-days", Usage: "Default retention days"},
					&cli.IntFlag{Name: "default-years", Usage: "Default retention years"},
				},
				Action: enableObjectLock,
			},
			{
				Name:      "get",
				Usage:     "Get bucket object lock configuration",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{serverFlag()},
				Action:    getObjectLock,
			},
		},
	}
}

func enableObjectLock(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	cfg := &bucket.ObjectLockConfig{Enabled: true}
	if mode := c.String("default-mode"); mode != "" {
		cfg.DefaultRetention = &bucket.DefaultRetention{
			Mode:  mode,
			Days:  c.Int("default-days"),
			Years: c.Int("default-years"),
		}
	}

	out, err := apiClient.PutBucketObjectLock(c.Args().First(), cfg)
	if err != nil {
		return fmt.Errorf("failed to enable object lock: %w", err)
	}
	printJSON("Object lock enabled:", out)
	return nil
}

func getObjectLock(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.GetBucketObjectLock(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to get object lock: %w", err)
	}
	printJSON("Object lock:", out)
	return nil
}
