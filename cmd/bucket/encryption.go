package bucket

import (
	"encoding/json"
	"fmt"

	"github.com/anhostfr/hangar/internal/client"
	"github.com/anhostfr/hangar/internal/service/bucket"
	"github.com/urfave/cli/v2"
)

func encryptionCommand() *cli.Command {
	return &cli.Command{
		Name:  "encryption",
		Usage: "Manage bucket default encryption (SSE-S3)",
		Subcommands: []*cli.Command{
			{
				Name:      "set",
				Usage:     "Set bucket default encryption (AES256 only)",
				ArgsUsage: "<bucket-name>",
				Flags: []cli.Flag{
					serverFlag(),
					&cli.StringFlag{Name: "algorithm", Value: "AES256", Usage: "Algorithm (AES256 only)"},
					&cli.StringFlag{Name: "kms-key-id", Usage: "KMS key ID (stored, not enforced)"},
				},
				Action: setEncryption,
			},
			{
				Name:      "get",
				Usage:     "Get bucket default encryption",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{serverFlag()},
				Action:    getEncryption,
			},
			{
				Name:      "delete",
				Usage:     "Remove bucket default encryption",
				ArgsUsage: "<bucket-name>",
				Flags:     []cli.Flag{serverFlag()},
				Action:    deleteEncryption,
			},
		},
	}
}

func setEncryption(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))
	cfg := &bucket.EncryptionConfig{Algorithm: c.String("algorithm"), KMSKeyID: c.String("kms-key-id")}

	out, err := apiClient.PutBucketEncryption(c.Args().First(), cfg)
	if err != nil {
		return fmt.Errorf("failed to set encryption: %w", err)
	}
	printJSON("Encryption set:", out)
	return nil
}

func getEncryption(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.GetBucketEncryption(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to get encryption: %w", err)
	}
	printJSON("Encryption:", out)
	return nil
}

func deleteEncryption(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}
	apiClient := client.NewClient(c.String("server"))

	if err := apiClient.DeleteBucketEncryption(c.Args().First()); err != nil {
		return fmt.Errorf("failed to delete encryption: %w", err)
	}
	fmt.Printf("Encryption removed from bucket '%s'\n", c.Args().First())
	return nil
}

func serverFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "server",
		Usage:   "Server URL",
		Aliases: []string{"s"},
		Value:   "http://localhost:8080",
	}
}

func printJSON(label string, v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Printf("%s\n%s\n", label, data)
}
