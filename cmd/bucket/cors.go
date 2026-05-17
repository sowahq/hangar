package bucket

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/anhostfr/hangar/internal/client"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/urfave/cli/v2"
)

func corsCommand() *cli.Command {
	return &cli.Command{
		Name:  "cors",
		Usage: "Manage CORS rules",
		Subcommands: []*cli.Command{
			{
				Name:      "set",
				Usage:     "Set CORS rules from a JSON file ({\"rules\":[...]})",
				ArgsUsage: "<bucket-name>",
				Flags: []cli.Flag{
					serverFlag(),
					&cli.StringFlag{Name: "file", Required: true, Aliases: []string{"f"}, Usage: "Path to JSON file with CORSConfiguration"},
				},
				Action: setCORS,
			},
			{
				Name:      "get",
				Usage:     "Get CORS configuration",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{serverFlag()},
				Action:    getCORS,
			},
			{
				Name:      "delete",
				Usage:     "Remove CORS configuration",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{serverFlag()},
				Action:    deleteCORS,
			},
		},
	}
}

func setCORS(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	data, err := os.ReadFile(c.String("file"))
	if err != nil {
		return fmt.Errorf("read --file: %w", err)
	}
	var cfg bucket.CORSConfiguration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse --file: %w", err)
	}

	apiClient := client.NewClient(c.String("server"))
	out, err := apiClient.PutBucketCORS(c.Args().First(), &cfg)
	if err != nil {
		return fmt.Errorf("failed to set cors: %w", err)
	}
	printJSON("CORS set:", out)
	return nil
}

func getCORS(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.GetBucketCORS(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to get cors: %w", err)
	}
	printJSON("CORS:", out)
	return nil
}

func deleteCORS(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	if err := apiClient.DeleteBucketCORS(c.Args().First()); err != nil {
		return fmt.Errorf("failed to delete cors: %w", err)
	}
	fmt.Printf("CORS removed from bucket '%s'\n", c.Args().First())
	return nil
}
