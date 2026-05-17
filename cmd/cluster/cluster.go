package cluster

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/anhostfr/hangar/internal/client"
	"github.com/urfave/cli/v2"
)

func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "status",
			Usage: "Show cluster status",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "server", Aliases: []string{"s"}, Value: "http://localhost:8080"},
			},
			Action: statusCmd,
		},
		{
			Name:  "node",
			Usage: "Manage cluster nodes",
			Subcommands: []*cli.Command{
				{
					Name:      "remove",
					Usage:     "Remove node from layout",
					ArgsUsage: "<node-id>",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: "server", Aliases: []string{"s"}, Value: "http://localhost:8080"},
					},
					Action: nodeRemove,
				},
				{
					Name:      "drain",
					Usage:     "Mark node as draining (HRW skip writes, finish reads)",
					ArgsUsage: "<node-id>",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: "server", Aliases: []string{"s"}, Value: "http://localhost:8080"},
					},
					Action: nodeDrain,
				},
			},
		},
		{
			Name:  "layout",
			Usage: "Inspect and apply cluster layout",
			Subcommands: []*cli.Command{
				{
					Name:  "show",
					Usage: "Show currently applied layout",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: "server", Aliases: []string{"s"}, Value: "http://localhost:8080"},
					},
					Action: layoutShow,
				},
				{
					Name:      "apply",
					Usage:     "Apply layout from JSON file (version must be > current)",
					ArgsUsage: "<path>",
					Flags: []cli.Flag{
						&cli.StringFlag{Name: "server", Aliases: []string{"s"}, Value: "http://localhost:8080"},
					},
					Action: layoutApply,
				},
			},
		},
	}
}

func statusCmd(c *cli.Context) error {
	cl := client.NewClient(c.String("server"))
	out, err := cl.GetClusterStatus()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func nodeRemove(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("node id required")
	}
	cl := client.NewClient(c.String("server"))
	out, err := cl.RemoveClusterNode(c.Args().First())
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func nodeDrain(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("node id required")
	}
	cl := client.NewClient(c.String("server"))
	out, err := cl.DrainClusterNode(c.Args().First())
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func layoutShow(c *cli.Context) error {
	cl := client.NewClient(c.String("server"))
	out, err := cl.GetClusterLayout()
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func layoutApply(c *cli.Context) error {
	if c.NArg() == 0 {
		return fmt.Errorf("layout file path required")
	}
	raw, err := os.ReadFile(c.Args().First())
	if err != nil {
		return fmt.Errorf("read layout file: %w", err)
	}
	cl := client.NewClient(c.String("server"))
	out, err := cl.ApplyClusterLayout(raw)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
