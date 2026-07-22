package bucket

import (
	"fmt"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/urfave/cli/v2"
)

func websiteCommand() *cli.Command {
	return &cli.Command{
		Name:  "website",
		Usage: "Manage static website hosting",
		Subcommands: []*cli.Command{
			{
				Name:      "set",
				Usage:     "Set website configuration",
				ArgsUsage: "<bucket-name>",
				Flags: []cli.Flag{
					common.ServerFlag(),
					&cli.StringFlag{Name: "index", Required: true, Usage: "Index document suffix (e.g. index.html)"},
					&cli.StringFlag{Name: "error", Usage: "Error document key (e.g. error.html)"},
				},
				Action: setWebsite,
			},
			{
				Name:      "get",
				Usage:     "Get website configuration",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{common.ServerFlag()},
				Action:    getWebsite,
			},
			{
				Name:      "delete",
				Usage:     "Disable static website hosting",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{common.ServerFlag()},
				Action:    deleteWebsite,
			},
		},
	}
}

func setWebsite(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))
	cfg := &bucket.WebsiteConfig{IndexDocument: c.String("index"), ErrorDocument: c.String("error")}

	out, err := apiClient.PutBucketWebsite(c.Args().First(), cfg)
	if err != nil {
		return fmt.Errorf("failed to set website: %w", err)
	}
	printJSON("Website set:", out)
	return nil
}

func getWebsite(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.GetBucketWebsite(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to get website: %w", err)
	}
	printJSON("Website:", out)
	return nil
}

func deleteWebsite(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	if err := apiClient.DeleteBucketWebsite(c.Args().First()); err != nil {
		return fmt.Errorf("failed to delete website: %w", err)
	}
	fmt.Printf("Website removed from bucket '%s'\n", c.Args().First())
	return nil
}
