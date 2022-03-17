package main

import (
	"flag"
	"log"
	"os"

	"github.com/0xPolygon/pbft-consensus/e2e/fuzz/replay"
	"github.com/mitchellh/cli"
)

type FuzzCLICommand interface {
	NewFlagSet() *flag.FlagSet
}

func main() {
	commands := getCommands()

	cli := &cli.CLI{
		Name:     "fuzz",
		Args:     os.Args[1:],
		Commands: commands,
	}

	_, err := cli.Run()
	if err != nil {
		log.Fatalf("Error executing CLI: %s\n", err.Error())
	}
}

func getCommands() map[string]cli.CommandFactory {
	ui := &cli.BasicUi{
		Reader:      os.Stdin,
		Writer:      os.Stdout,
		ErrorWriter: os.Stderr,
	}
	return map[string]cli.CommandFactory{
		"fuzz-run": func() (cli.Command, error) {
			return &FuzzCommand{
				UI: ui,
			}, nil
		},
		"replay-messages": func() (cli.Command, error) {
			return &replay.ReplayMessageCommand{
				UI: ui,
			}, nil
		},
	}
}
