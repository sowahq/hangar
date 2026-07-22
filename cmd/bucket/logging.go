package bucket

import (
	"fmt"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/urfave/cli/v2"
)

func loggingCommand() *cli.Command {
	return &cli.Command{
		Name:  "logging",
		Usage: "Manage bucket access logging",
		Subcommands: []*cli.Command{
			{
				Name:      "set",
				Usage:     "Enable access logging to a target bucket",
				ArgsUsage: "<bucket-name>",
				Flags: []cli.Flag{
					common.ServerFlag(),
					&cli.StringFlag{Name: "target-bucket", Required: true, Usage: "Target bucket to receive log objects"},
					&cli.StringFlag{Name: "target-prefix", Usage: "Prefix for log object keys"},
				},
				Action: setLogging,
			},
			{
				Name:      "get",
				Usage:     "Get access logging configuration",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{common.ServerFlag()},
				Action:    getLogging,
			},
			{
				Name:      "delete",
				Usage:     "Disable access logging",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{common.ServerFlag()},
				Action:    deleteLogging,
			},
		},
	}
}

func setLogging(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))
	cfg := &bucket.LoggingConfig{TargetBucket: c.String("target-bucket"), TargetPrefix: c.String("target-prefix")}

	out, err := apiClient.PutBucketLogging(c.Args().First(), cfg)
	if err != nil {
		return fmt.Errorf("failed to set logging: %w", err)
	}
	printJSON("Logging set:", out)
	return nil
}

func getLogging(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.GetBucketLogging(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to get logging: %w", err)
	}
	printJSON("Logging:", out)
	return nil
}

func deleteLogging(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	if err := apiClient.DeleteBucketLogging(c.Args().First()); err != nil {
		return fmt.Errorf("failed to delete logging: %w", err)
	}
	fmt.Printf("Logging disabled for bucket '%s'\n", c.Args().First())
	return nil
}
