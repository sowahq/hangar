package cluster

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
			Name:  "init",
			Usage: "Generate a fresh shared_secret_b64 and scaffold a cluster TOML block",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "listen", Value: ":7000", Usage: "Cluster RPC listen address"},
				&cli.StringSliceFlag{Name: "seed", Usage: "Seed host:port (repeat for multiple). Omit on the bootstrap node."},
				&cli.StringFlag{Name: "node-id", Usage: "Node id override (default: hostname)"},
				&cli.StringFlag{Name: "zone", Usage: "Optional zone label"},
				&cli.Int64Flag{Name: "capacity", Usage: "Optional capacity weight"},
				&cli.StringFlag{Name: "out", Aliases: []string{"o"}, Usage: "Write to file instead of stdout"},
			},
			Action: initCmd,
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

func initCmd(c *cli.Context) error {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Errorf("generate secret: %w", err)
	}
	secret := base64.StdEncoding.EncodeToString(raw[:])

	var b strings.Builder
	b.WriteString("[cluster]\n")
	b.WriteString("enabled = true\n")
	fmt.Fprintf(&b, "listen = %q\n", c.String("listen"))
	fmt.Fprintf(&b, "shared_secret_b64 = %q\n", secret)
	if id := c.String("node-id"); id != "" {
		fmt.Fprintf(&b, "node_id = %q\n", id)
	}
	if zone := c.String("zone"); zone != "" {
		fmt.Fprintf(&b, "zone = %q\n", zone)
	}
	if cap := c.Int64("capacity"); cap > 0 {
		fmt.Fprintf(&b, "capacity = %d\n", cap)
	}
	if seeds := c.StringSlice("seed"); len(seeds) > 0 {
		quoted := make([]string, len(seeds))
		for i, s := range seeds {
			quoted[i] = fmt.Sprintf("%q", s)
		}
		fmt.Fprintf(&b, "seeds = [%s]\n", strings.Join(quoted, ", "))
	}

	out := b.String()
	if path := c.String("out"); path != "" {
		if err := os.WriteFile(path, []byte(out), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %s (mode 0600)\n", path)
		return nil
	}
	fmt.Print(out)
	return nil
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
