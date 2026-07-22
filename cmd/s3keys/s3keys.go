package s3keys

import (
	"encoding/json"
	"fmt"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/urfave/cli/v2"
)

func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "create",
			Usage: "Create a new S3 access key",
			Flags: []cli.Flag{
				common.ServerFlag(),
				&cli.StringSliceFlag{
					Name:     "perm",
					Usage:    "Permission (read, write, delete, admin). Repeatable.",
					Aliases:  []string{"p"},
					Required: true,
				},
				&cli.StringSliceFlag{
					Name:    "bucket",
					Usage:   "Restrict to bucket. Repeatable. Empty = all buckets.",
					Aliases: []string{"b"},
				},
			},
			Action: createS3Key,
		},
		{
			Name:      "update",
			Usage:     "Replace permissions and bucket scope of an S3 access key",
			ArgsUsage: "<access-key-id>",
			Flags: []cli.Flag{
				common.ServerFlag(),
				&cli.StringSliceFlag{
					Name:     "perm",
					Usage:    "Permission (read, write, delete, admin). Repeatable. Replaces existing.",
					Aliases:  []string{"p"},
					Required: true,
				},
				&cli.StringSliceFlag{
					Name:    "bucket",
					Usage:   "Restrict to bucket. Repeatable. Empty = all buckets. Replaces existing.",
					Aliases: []string{"b"},
				},
			},
			Action: updateS3Key,
		},
		{
			Name:   "list",
			Usage:  "List all S3 access keys",
			Flags:  []cli.Flag{common.ServerFlag()},
			Action: listS3Keys,
		},
		{
			Name:      "delete",
			Usage:     "Delete an S3 access key",
			ArgsUsage: "<access-key-id>",
			Flags:     []cli.Flag{common.ServerFlag()},
			Action:    deleteS3Key,
		},
	}
}

func createS3Key(c *cli.Context) error {
	apiClient := client.NewClient(c.String("server"))
	req := &client.CreateS3KeyRequest{
		Permissions: c.StringSlice("perm"),
		Buckets:     c.StringSlice("bucket"),
	}
	result, err := apiClient.CreateS3Key(req)
	if err != nil {
		return fmt.Errorf("failed to create s3 key: %w", err)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("S3 access key created (save the secret_key NOW, it will not be shown again):\n%s\n", data)
	return nil
}

func listS3Keys(c *cli.Context) error {
	apiClient := client.NewClient(c.String("server"))
	result, err := apiClient.ListS3Keys()
	if err != nil {
		return fmt.Errorf("failed to list s3 keys: %w", err)
	}
	if result.Count == 0 {
		fmt.Println("No S3 keys found")
		return nil
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("Found %d S3 key(s):\n%s\n", result.Count, data)
	return nil
}

func updateS3Key(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("access key id is required")
	}
	id := c.Args().First()
	apiClient := client.NewClient(c.String("server"))
	req := &client.UpdateS3KeyRequest{
		Permissions: c.StringSlice("perm"),
		Buckets:     c.StringSlice("bucket"),
	}
	result, err := apiClient.UpdateS3Key(id, req)
	if err != nil {
		return fmt.Errorf("failed to update s3 key: %w", err)
	}
	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("S3 access key updated:\n%s\n", data)
	return nil
}

func deleteS3Key(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("access key id is required")
	}
	id := c.Args().First()
	apiClient := client.NewClient(c.String("server"))
	if err := apiClient.DeleteS3Key(id); err != nil {
		return fmt.Errorf("failed to delete s3 key: %w", err)
	}
	fmt.Printf("S3 key '%s' deleted successfully\n", id)
	return nil
}
