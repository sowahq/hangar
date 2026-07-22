package tokens

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
			Name:      "create",
			Usage:     "Create a new access token for a bucket",
			ArgsUsage: "<bucket-name>",
			Flags: []cli.Flag{
				common.ServerFlag(),
				&cli.StringSliceFlag{
					Name:     "perm",
					Usage:    "Permission (read, write, delete). Repeatable.",
					Aliases:  []string{"p"},
					Required: true,
				},
			},
			Action: createToken,
		},
		{
			Name:      "list",
			Usage:     "List tokens for a bucket",
			ArgsUsage: "<bucket-name>",
			Flags:     []cli.Flag{common.ServerFlag()},
			Action:    listTokens,
		},
		{
			Name:      "delete",
			Usage:     "Delete a token from a bucket",
			ArgsUsage: "<bucket-name> <token-id>",
			Flags:     []cli.Flag{common.ServerFlag()},
			Action:    deleteToken,
		},
	}
}

func createToken(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}

	apiClient := client.NewClient(c.String("server"))

	result, err := apiClient.CreateToken(c.Args().First(), c.StringSlice("perm"))
	if err != nil {
		return fmt.Errorf("failed to create token: %w", err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("Token created (save it NOW, it will not be shown again):\n%s\n", data)

	return nil
}

func listTokens(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("bucket name is required")
	}

	apiClient := client.NewClient(c.String("server"))

	result, err := apiClient.ListTokens(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to list tokens: %w", err)
	}

	if result.Count == 0 {
		fmt.Println("No tokens found")
		return nil
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	fmt.Printf("Found %d token(s):\n%s\n", result.Count, data)

	return nil
}

func deleteToken(c *cli.Context) error {
	if c.NArg() < 2 {
		return fmt.Errorf("bucket name and token id are required")
	}

	apiClient := client.NewClient(c.String("server"))

	bucketName := c.Args().Get(0)
	id := c.Args().Get(1)

	if err := apiClient.DeleteToken(bucketName, id); err != nil {
		return fmt.Errorf("failed to delete token: %w", err)
	}

	fmt.Printf("Token '%s' deleted successfully\n", id)

	return nil
}
