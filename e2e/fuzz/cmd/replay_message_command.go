package main

import (
	"flag"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/0xPolygon/pbft-consensus/e2e/fuzz/types"
	"github.com/mitchellh/cli"
)

//Struct containing data for running replay-message command
type ReplayMessageCommand struct {
	UI cli.Ui

	filePath string
	duration time.Duration
}

// Help implements the cli.Command interface
func (fc *ReplayMessageCommand) Help() string {
	return `Runs the message and timeouts replay for analysis and testing purposes based on provided .flow file.
	
	Usage: replay-messages -file={fullPathToFlowFile}
	
	Options:
	
	-file - Full path to .flow file containing messages and timeouts to be replayed by the fuzz framework
	-duration - Duration of fuzz daemon running, must be longer than 1 minute (e.g., 2m, 5m, 1h, 2h)`
}

// Synopsis implements the cli.Command interface
func (rmc *ReplayMessageCommand) Synopsis() string {
	return "Starts the replay of messages and timeouts in fuzz runner"
}

// Run implements the cli.Command interface and runs the command
func (rmc *ReplayMessageCommand) Run(args []string) int {
	flagSet := rmc.NewFlagSet()
	err := flagSet.Parse(args)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	var nodeNames []string
	var messages []*types.ReplayMessage
	if rmc.filePath != "" {
		messages, nodeNames, err = types.Load(rmc.filePath)
		if err != nil {
			log.Fatalf("Error while reading replay messages file. Error: %v", err)
		}
	}

	nodesCount := len(nodeNames)
	if nodesCount == 0 {
		log.Println("No nodes were found in .flow file, so no cluster will be started")
		return 0
	}

	if len(messages) == 0 {
		log.Println("No messages were loaded from .flow file, so no cluster will be started")
		return 0
	}

	i := strings.Index(nodeNames[0], "_")
	prefix := ""
	if i > -1 {
		prefix = (nodeNames[0])[:i]
	}
	stateHandler := &types.ReplayMessagesHandler{}
	config := &e2e.ClusterConfig{
		Count:        nodesCount,
		Name:         "fuzz_cluster",
		Prefix:       prefix,
		StateHandler: stateHandler,
	}

	cluster := e2e.NewPBFTCluster(nil, config)

	nodes := cluster.GetNodesMap()
	for _, message := range messages {
		node, exists := nodes[string(message.To)]
		if !exists {
			log.Printf("[WARNING] Could not find node: %v to push message from .flow file", message.To)
		} else if message.Message != nil {
			if message.To != message.Message.From || message.Message.Type == pbft.MessageReq_Preprepare {
				node.PushMessage(message.Message)
			}
		} else {
			//TO DO - HANDLE TIMEOUT
		}
	}

	wg := sync.WaitGroup{}
	wg.Add(1)

	cluster.Start()
	defer cluster.Stop()

	done := time.After(rmc.duration)
	go func() {
		defer wg.Done()
		<-done
		log.Println("Done with execution")
	}()

	wg.Wait()
	stateHandler.CloseFile()

	return 0
}

// NewFlagSet implements the FuzzCLICommand interface and creates a new flag set for command arguments
func (rmc *ReplayMessageCommand) NewFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet("replay-messages", flag.ContinueOnError)
	flagSet.StringVar(&rmc.filePath, "file", "", "Full path to .flow file containing messages and timeouts to be replayed by the fuzz framework")
	flagSet.DurationVar(&rmc.duration, "duration", 25*time.Minute, "Duration of fuzz daemon running")

	return flagSet
}
