package main

import (
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e/fuzz"
	"github.com/0xPolygon/pbft-consensus/e2e/fuzz/types"
	"github.com/mitchellh/cli"
)

//Struct containing data for running fuzz-run command
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

	log.Printf("Starting PolyBFT fuzz runner...")
	log.Printf("Node count: %v\n", fc.numberOfNodes)
	log.Printf("Duration: %v\n", fc.duration)
	rand.Seed(time.Now().Unix())

	stateHandler := &types.ReplayMessagesHandler{}
	runner := fuzz.NewRunner(fc.numberOfNodes, stateHandler)
	err = runner.Run(fc.duration)
	if err != nil {
		log.Printf("Error while running PolyBFT fuzz runner: '%s'\n", err)
	} else {
		log.Println("PolyBFT fuzz runner is stopped.")
	}
	stateHandler.CloseFile()

	return 0
}

// NewFlagSet implements the FuzzCLICommand interface and creates a new flag set for command arguments
func (fc *FuzzCommand) NewFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet("fuzz-run", flag.ContinueOnError)
	flagSet.UintVar(&fc.numberOfNodes, "nodes", 5, "Count of initially started nodes")
	flagSet.DurationVar(&fc.duration, "duration", 25*time.Minute, "Duration of fuzz daemon running")

	return flagSet
}
