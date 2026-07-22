package sse

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/urfave/cli/v2"
)

func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:   "list",
			Usage:  "List SSE-S3 encryption keys",
			Flags:  []cli.Flag{common.ServerFlag()},
			Action: listKeys,
		},
		{
			Name:   "rotate",
			Usage:  "Generate a new active SSE-S3 key",
			Flags:  []cli.Flag{common.ServerFlag()},
			Action: rotateKey,
		},
		{
			Name:      "activate",
			Usage:     "Set an existing SSE-S3 key as active",
			ArgsUsage: "<key-id>",
			Flags:     []cli.Flag{common.ServerFlag()},
			Action:    activateKey,
		},
	}
}

func listKeys(c *cli.Context) error {
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.ListSSEKeys()
	if err != nil {
		return fmt.Errorf("failed to list sse keys: %w", err)
	}

	return printJSON(out)
}

func rotateKey(c *cli.Context) error {
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.RotateSSEKey()
	if err != nil {
		return fmt.Errorf("failed to rotate sse key: %w", err)
	}

	return printJSON(out)
}

func activateKey(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("key id is required")
	}

	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.ActivateSSEKey(c.Args().First())
	if err != nil {
		return fmt.Errorf("failed to activate sse key: %w", err)
	}

	return printJSON(out)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
