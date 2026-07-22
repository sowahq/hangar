package backup

import (
	"encoding/json"
	"fmt"

	"github.com/BurntSushi/toml"
	backupsvc "github.com/sowahq/hangar/internal/service/backup"
	"github.com/urfave/cli/v2"
)

type minimalConfig struct {
	DataDirectory string `toml:"data_directory"`
}

func Commands() []*cli.Command {
	return []*cli.Command{
		{
			Name:  "create",
			Usage: "Create a consistent backup of data and chunks (server must be stopped)",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "config",
					Aliases: []string{"c"},
					Usage:   "Path to the configuration file",
					Value:   "config.toml",
				},
				&cli.StringFlag{
					Name:     "output",
					Aliases:  []string{"o"},
					Usage:    "Destination directory (must not exist)",
					Required: true,
				},
			},
			Action: createBackup,
		},
		{
			Name:  "restore",
			Usage: "Restore a backup into the configured data directory (must be empty)",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "config",
					Aliases: []string{"c"},
					Usage:   "Path to the configuration file",
					Value:   "config.toml",
				},
				&cli.StringFlag{
					Name:     "input",
					Aliases:  []string{"i"},
					Usage:    "Source backup directory",
					Required: true,
				},
			},
			Action: restoreBackup,
		},
	}
}

func loadDataDir(path string) (string, error) {
	var cfg minimalConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return "", fmt.Errorf("load config %s: %w", path, err)
	}

	if cfg.DataDirectory == "" {
		return "", fmt.Errorf("data_directory is empty in %s", path)
	}

	return cfg.DataDirectory, nil
}

func createBackup(c *cli.Context) error {
	dataDir, err := loadDataDir(c.String("config"))
	if err != nil {
		return err
	}

	out := c.String("output")
	m, err := backupsvc.Create(dataDir, out)
	if err != nil {
		return fmt.Errorf("backup failed: %w", err)
	}

	data, _ := json.MarshalIndent(m, "", "  ")
	fmt.Printf("Backup written to %s:\n%s\n", out, data)
	return nil
}

func restoreBackup(c *cli.Context) error {
	dataDir, err := loadDataDir(c.String("config"))
	if err != nil {
		return err
	}

	in := c.String("input")
	m, err := backupsvc.Restore(in, dataDir)
	if err != nil {
		return fmt.Errorf("restore failed: %w", err)
	}

	data, _ := json.MarshalIndent(m, "", "  ")
	fmt.Printf("Backup %s restored into %s:\n%s\n", in, dataDir, data)
	return nil
}
