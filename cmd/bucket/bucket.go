package bucket

import (
	"encoding/json"
	"fmt"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/sowahq/hangar/internal/service/bucket"
	"github.com/urfave/cli/v2"
)

func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:      "create",
			Usage:     "Create a new bucket",
			ArgsUsage: "<bucket-name>",
			Flags: []cli.Flag{
				common.ServerFlag(),
				&cli.BoolFlag{
					Name:  "public",
					Usage: "Make bucket public",
				},
			},
			Action: createBucket,
		},
		{
			Name:  "list",
			Usage: "List all buckets",
			Flags: []cli.Flag{
				common.ServerFlag(),
			},
			Action: listBuckets,
		},
		{
			Name:      "get",
			Usage:     "Get bucket information",
			ArgsUsage: "<bucket-name>",
			Flags: []cli.Flag{
				common.ServerFlag(),
			},
			Action: getBucket,
		},
		{
			Name:      "delete",
			Usage:     "Delete a bucket",
			ArgsUsage: "<bucket-name>",
			Flags: []cli.Flag{
				common.ServerFlag(),
				&cli.BoolFlag{
					Name:  "force",
					Usage: "Force delete bucket even if not empty",
				},
			},
			Action: deleteBucket,
		},
		{
			Name:      "quota",
			Usage:     "Set bucket quota (0 = unlimited)",
			ArgsUsage: "<bucket-name>",
			Flags: []cli.Flag{
				common.ServerFlag(),
				&cli.Int64Flag{
					Name:  "max-bytes",
					Usage: "Maximum total size in bytes",
				},
				&cli.Int64Flag{
					Name:  "max-objects",
					Usage: "Maximum number of objects",
				},
			},
			Action: updateQuota,
		},
		encryptionCommand(),
		objectLockCommand(),
		websiteCommand(),
		loggingCommand(),
		taggingCommand(),
		corsCommand(),
		lifecycleCommand(),
		versioningCommand(),
	}
}

func createBucket(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}

	serverURL := c.String("server")
	apiClient := client.NewClient(serverURL)

	req := &bucket.CreateBucketRequest{
		Name:   c.Args().First(),
		Public: c.Bool("public"),
	}

	result, err := apiClient.CreateBucket(req)
	if err != nil {
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("Bucket created successfully:\n%s\n", data)

	return nil
}

func listBuckets(c *cli.Context) error {
	serverURL := c.String("server")
	apiClient := client.NewClient(serverURL)

	result, err := apiClient.ListBuckets()
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	if result.Count == 0 {
		fmt.Println("No buckets found")
		return nil
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("Found %d bucket(s):\n%s\n", result.Count, data)

	return nil
}

func getBucket(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}

	serverURL := c.String("server")
	apiClient := client.NewClient(serverURL)

	name := c.Args().First()
	result, err := apiClient.GetBucket(name)
	if err != nil {
		return fmt.Errorf("failed to get bucket: %w", err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("Bucket information:\n%s\n", data)

	return nil
}

func updateQuota(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}

	serverURL := c.String("server")
	apiClient := client.NewClient(serverURL)

	name := c.Args().First()
	result, err := apiClient.UpdateBucketQuota(name, c.Int64("max-bytes"), c.Int64("max-objects"))
	if err != nil {
		return fmt.Errorf("failed to update quota: %w", err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("Quota updated:\n%s\n", data)

	return nil
}

func deleteBucket(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}

	serverURL := c.String("server")
	apiClient := client.NewClient(serverURL)

	name := c.Args().First()
	force := c.Bool("force")

	if err := apiClient.DeleteBucket(name, force); err != nil {
		return fmt.Errorf("failed to delete bucket: %w", err)
	}

	fmt.Printf("Bucket '%s' deleted successfully\n", name)

	return nil
}
