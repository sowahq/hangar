package status

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sowahq/hangar/cmd/common"
	"github.com/sowahq/hangar/internal/client"
	"github.com/urfave/cli/v2"
)

// Command returns the top-level status command.
func Command() *cli.Command {
	return &cli.Command{
		Name:   "status",
		Usage:  "Show server health and status",
		Flags:  []cli.Flag{common.ServerFlag()},
		Action: showStatus,
	}
}

func showStatus(c *cli.Context) error {
	apiClient := client.NewClient(c.String("server"))

	out, err := apiClient.GetStatus()
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")

	return enc.Encode(out)
}
