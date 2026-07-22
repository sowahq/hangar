package bucket

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/urfave/cli/v2"
)

func lifecycleCommand() *cli.Command {
	return &cli.Command{
		Name:  "lifecycle",
		Usage: "Manage lifecycle rules (expiration, abort multipart)",
		Subcommands: []*cli.Command{
			{
				Name:      "set",
				Usage:     "Set lifecycle rules from a JSON file ({\"rules\":[...]})",
				ArgsUsage: "<bucket-name>",
				Flags: []cli.Flag{
					common.ServerFlag(),
					&cli.StringFlag{Name: "file", Required: true, Aliases: []string{"f"}, Usage: "Path to JSON file with LifecycleConfiguration"},
				},
				Action: setLifecycle,
			},
			{
				Name:      "get",
				Usage:     "Get lifecycle configuration",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{common.ServerFlag()},
				Action:    getLifecycle,
			},
			{
				Name:      "delete",
				Usage:     "Remove lifecycle configuration",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{common.ServerFlag()},
				Action:    deleteLifecycle,
			},
			{
				Name:   "run",
				Usage:  "Trigger an immediate lifecycle sweep across all buckets",
				Flags:  []cli.Flag{common.ServerFlag()},
				Action: runLifecycle,
			},
		},
	}
}

func runLifecycle(c *cli.Context) error {
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.RunLifecycle()
	if err != nil {
		return fmt.Errorf("failed to run lifecycle: %w", err)
	}

	printJSON("Lifecycle run:", out)

	return nil
}

func setLifecycle(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	data, err := os.ReadFile(c.String("file"))
	if err != nil {
		return fmt.Errorf("read --file: %w", err)
	}
	var cfg bucket.LifecycleConfiguration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse --file: %w", err)
	}

	apiClient := client.NewClient(c.String("server"))
	out, err := apiClient.PutBucketLifecycle(c.Args().First(), &cfg)
	if err != nil {
		return fmt.Errorf("failed to set lifecycle: %w", err)
	}
	printJSON("Lifecycle set:", out)
	return nil
}

func getLifecycle(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.GetBucketLifecycle(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to get lifecycle: %w", err)
	}
	printJSON("Lifecycle:", out)
	return nil
}

func deleteLifecycle(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	if err := apiClient.DeleteBucketLifecycle(c.Args().First()); err != nil {
		return fmt.Errorf("failed to delete lifecycle: %w", err)
	}
	fmt.Printf("Lifecycle removed from bucket '%s'\n", c.Args().First())
	return nil
}
