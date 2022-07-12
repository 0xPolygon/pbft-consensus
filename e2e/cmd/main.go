package main

import (
	"log"
	"os"

	"github.com/mitchellh/cli"

	"github.com/0xPolygon/pbft-consensus/e2e/cmd/fuzz"
	"github.com/0xPolygon/pbft-consensus/e2e/cmd/replay"
)

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

// getCommands returns all registered commands
func getCommands() map[string]cli.CommandFactory {
	ui := &cli.BasicUi{
		Reader:      os.Stdin,
		Writer:      os.Stdout,
		ErrorWriter: os.Stderr,
	}
	return map[string]cli.CommandFactory{
		"fuzz-run": func() (cli.Command, error) {
			return fuzz.New(ui), nil
		},
		"replay-messages": func() (cli.Command, error) {
			return replay.New(ui), nil
		},
	}
}
