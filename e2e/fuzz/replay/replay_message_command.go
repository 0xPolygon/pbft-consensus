package replay

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/mitchellh/cli"
)

// ReplayMessageCommand is a struct containing data for running replay-message command
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
	err := rmc.validateInput(args)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	messageReader := &replayMessageReader{
		msgProcessingDone: make(chan string),
	}

	err = messageReader.openFile(rmc.filePath)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	nodeNames, err := messageReader.readNodeMetaData()
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	i := strings.Index(nodeNames[0], "_")
	prefix := ""
	if i > -1 {
		prefix = (nodeNames[0])[:i]
	}

	replayMessagesNotifier := NewReplayMessagesNotifierWithReader(messageReader)

	nodesCount := len(nodeNames)
	config := &e2e.ClusterConfig{
		Count:                 nodesCount,
		Name:                  "fuzz_cluster",
		Prefix:                prefix,
		ReplayMessageNotifier: replayMessagesNotifier,
		RoundTimeout:          roundTimeout,
		TransportHandler:      func(to pbft.NodeID, msg *pbft.MessageReq) { replayMessagesNotifier.HandleMessage(to, msg) },
	}

	cluster := e2e.NewPBFTCluster(nil, config)

	messageReader.readMessages(cluster)
	messageReader.closeFile()

	var wg sync.WaitGroup
	wg.Add(1)

	nodesDone := make(map[string]bool, nodesCount)
	go func() {
		for {
			select {
			case nodeDone := <-messageReader.msgProcessingDone:
				nodesDone[nodeDone] = true
				cluster.StopNode(nodeDone)
				if len(nodesDone) == nodesCount {
					wg.Done()
					return
				}
			default:
				continue
			}
		}
	}()

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

// validateInput parses arguments from CLI and validates their correctness
func (rmc *ReplayMessageCommand) validateInput(args []string) error {
	flagSet := rmc.NewFlagSet()
	err := flagSet.Parse(args)
	if err != nil {
		return err
	}

	if rmc.filePath == "" {
		err = errors.New("provided file path is empty")
		return err
	}
	return nil
}
