package bucket

import (
	"fmt"
	"strings"

	"github.com/anhostfr/hangar/internal/client"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/anhostfr/hangar/internal/storage"
	"github.com/urfave/cli/v2"
)

func taggingCommand() *cli.Command {
	return &cli.Command{
		Name:  "tagging",
		Usage: "Manage bucket tags",
		Subcommands: []*cli.Command{
			{
				Name:      "set",
				Usage:     "Set bucket tags (replaces all)",
				ArgsUsage: "<bucket-name>",
				Flags: []cli.Flag{
					serverFlag(),
					&cli.StringSliceFlag{Name: "tag", Required: true, Aliases: []string{"t"}, Usage: "Tag in key=value format (repeatable)"},
				},
				Action: setTagging,
			},
			{
				Name:      "get",
				Usage:     "Get bucket tags",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{serverFlag()},
				Action:    getTagging,
			},
			{
				Name:      "delete",
				Usage:     "Remove all bucket tags",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{serverFlag()},
				Action:    deleteTagging,
			},
		},
	}
}

func setTagging(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	pairs := c.StringSlice("tag")
	tags := make([]bucket.Tag, 0, len(pairs))
	for _, p := range pairs {
		idx := strings.IndexByte(p, '=')
		if idx <= 0 {
			return fmt.Errorf("invalid --tag %q: expected key=value", p)
		}
		tags = append(tags, storage.Tag{Key: p[:idx], Value: p[idx+1:]})
	}

	out, err := apiClient.PutBucketTagging(c.Args().First(), tags)
	if err != nil {
		return fmt.Errorf("failed to set tagging: %w", err)
	}
	printJSON("Tags set:", out)
	return nil
}

func getTagging(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.GetBucketTagging(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to get tagging: %w", err)
	}
	printJSON("Tags:", out)
	return nil
}

func deleteTagging(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	if err := apiClient.DeleteBucketTagging(c.Args().First()); err != nil {
		return fmt.Errorf("failed to delete tagging: %w", err)
	}
	fmt.Printf("Tags removed from bucket '%s'\n", c.Args().First())
	return nil
}
