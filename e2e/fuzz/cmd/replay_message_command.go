package main

import (
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/0xPolygon/pbft-consensus/e2e/fuzz/replay"
	"github.com/mitchellh/cli"
)

//ReplayMessageCommand is a struct containing data for running replay-message command
type ReplayMessageCommand struct {
	UI cli.Ui

	filePath string
}

// Help implements the cli.Command interface
func (fc *ReplayMessageCommand) Help() string {
	return `Runs the message and timeouts replay for analysis and testing purposes based on provided .flow file.
	
	Usage: replay-messages -file={fullPathToFlowFile}
	
	Options:
	
	-file - Full path to .flow file containing messages and timeouts to be replayed by the fuzz framework`
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
	var messages []*replay.ReplayMessage
	if rmc.filePath != "" {
		messages, nodeNames, err = replay.Load(rmc.filePath)
		if err != nil {
			rmc.UI.Error(fmt.Sprintf("Error while reading replay messages file. Error: %v", err))
			return 1
		}
	}

	nodesCount := len(nodeNames)
	if nodesCount == 0 {
		rmc.UI.Error("No nodes were found in .flow file, so no cluster will be started")
		return 1
	}

	if len(messages) == 0 {
		rmc.UI.Error("No messages were loaded from .flow file, so no cluster will be started")
		return 1
	}

	i := strings.Index(nodeNames[0], "_")
	prefix := ""
	if i > -1 {
		prefix = (nodeNames[0])[:i]
	}

	replayMessagesNotifier := replay.NewReplayMessagesNotifier(nodesCount)
	config := &e2e.ClusterConfig{
		Count:                 nodesCount,
		Name:                  "fuzz_cluster",
		Prefix:                prefix,
		ReplayMessageNotifier: replayMessagesNotifier,
		RoundTimeout:          roundTimeout,
		TransportHandler:      func(to pbft.NodeID, msg *pbft.MessageReq) { replayMessagesNotifier.HandleMessage(to, msg) },
	}

	cluster := e2e.NewPBFTCluster(nil, config)

	nodes := cluster.GetNodesMap()
	for _, message := range messages {
		node, exists := nodes[string(message.To)]
		if !exists {
			rmc.UI.Warn(fmt.Sprintf("Could not find node: %v to push message from .flow file", message.To))
		} else if message.To != message.Message.From || message.Message.Type == pbft.MessageReq_Preprepare {
			// since timeouts have .From property always empty, and To always non empty, they will be pushed to message queue
			// for regular messages we will only push those where sender and receiver differ or if its a PrePrepare message
			node.PushMessage(message.Message)
		}
	}

	var wg sync.WaitGroup
	wg.Add(nodesCount)

	for i := 0; i < nodesCount; i++ {
		go func() {
			defer wg.Done()
			<-replayMessagesNotifier.Channel
		}()
	}

	cluster.Start()
	wg.Wait()
	cluster.Stop()

	rmc.UI.Info("Done with execution")
	if err = replayMessagesNotifier.CloseFile(); err != nil {
		rmc.UI.Error(fmt.Sprintf("Error while closing .flow file: '%s'\n", err))
		return 1
	}

	return 0
}

// NewFlagSet implements the FuzzCLICommand interface and creates a new flag set for command arguments
func (rmc *ReplayMessageCommand) NewFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet("replay-messages", flag.ContinueOnError)
	flagSet.StringVar(&rmc.filePath, "file", "", "Full path to .flow file containing messages and timeouts to be replayed by the fuzz framework")

	return flagSet
}

// roundTimeout is an implementation of roundTimeout in pbft
func roundTimeout(round uint64) time.Duration {
	return time.Millisecond
}
