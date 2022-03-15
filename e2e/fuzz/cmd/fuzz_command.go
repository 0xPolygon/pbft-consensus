package main

import (
	"flag"
	"fmt"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e/fuzz"
	"github.com/0xPolygon/pbft-consensus/e2e/fuzz/replay"
	"github.com/mitchellh/cli"
)

//FuzzCommand is a struct containing data for running fuzz-run command
type FuzzCommand struct {
	UI cli.Ui

	numberOfNodes uint
	duration      time.Duration
}

// Help implements the cli.Command interface
func (fc *FuzzCommand) Help() string {
	return `Command runs the fuzz runner in fuzz framework based on provided configuration (nodes count and duration).
	
	Usage: fuzz-run -nodes={numberOfNodes} -duration={duration}
	
	Options:
	
	-nodes - Count of initially started nodes
	-duration - Duration of fuzz daemon running, must be longer than 1 minute (e.g., 2m, 5m, 1h, 2h)`
}

// Synopsis implements the cli.Command interface
func (fc *FuzzCommand) Synopsis() string {
	return "Starts the PolyBFT fuzz runner"
}

// Run implements the cli.Command interface and runs the command
func (fc *FuzzCommand) Run(args []string) int {
	flagSet := fc.NewFlagSet()
	err := flagSet.Parse(args)
	if err != nil {
		fc.UI.Error(err.Error())
		return 1
	}

	fc.UI.Info("Starting PolyBFT fuzz runner...")
	fc.UI.Info(fmt.Sprintf("Node count: %v\n", fc.numberOfNodes))
	fc.UI.Info(fmt.Sprintf("Duration: %v\n", fc.duration))
	rand.Seed(time.Now().Unix())

	replayMessageHandler := &replay.ReplayMessagesNotifier{}
	runner := fuzz.NewRunner(fc.numberOfNodes, replayMessageHandler)
	err = runner.Run(fc.duration)
	if err != nil {
		fc.UI.Error(fmt.Sprintf("Error while running PolyBFT fuzz runner: '%s'\n", err))
	} else {
		fc.UI.Info("PolyBFT fuzz runner is stopped.")
	}

	if err = replayMessageHandler.CloseFile(); err != nil {
		fc.UI.Error(fmt.Sprintf("Error while closing .flow file: '%s'\n", err))
		return 1
	}

	return 0
}

// NewFlagSet implements the FuzzCLICommand interface and creates a new flag set for command arguments
func (fc *FuzzCommand) NewFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet("fuzz-run", flag.ContinueOnError)
	flagSet.UintVar(&fc.numberOfNodes, "nodes", 5, "Count of initially started nodes")
	flagSet.DurationVar(&fc.duration, "duration", 25*time.Minute, "Duration of fuzz daemon running")

	return flagSet
}
