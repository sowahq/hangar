package main

import (
	"os"

	"github.com/anhostfr/hangar/cmd/root"
	"github.com/phuslu/log"
)

func init() {
	if log.IsTerminal(os.Stderr.Fd()) {
		log.DefaultLogger = log.Logger{
			TimeFormat: "15:04:05",
			Caller:     0,
			Writer: &log.ConsoleWriter{
				ColorOutput:    true,
				QuoteString:    true,
				EndWithMessage: true,
			},
		}
	}
}

func main() {
	root.Execute()
}
