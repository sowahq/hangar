package common

import (
	"github.com/urfave/cli/v2"
)

// ServerFlag returns the shared --server flag used by every admin command.
func ServerFlag() cli.Flag {
	return &cli.StringFlag{
		Name:    "server",
		Usage:   "Server URL",
		Aliases: []string{"s"},
		Value:   "http://localhost:8080",
		EnvVars: []string{"HANGAR_SERVER"},
	}
}
