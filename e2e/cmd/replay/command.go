package replay

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mitchellh/cli"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/0xPolygon/pbft-consensus/e2e/replay"
)

// MessageCommand is a struct containing data for running replay-message command
type MessageCommand struct {
	UI cli.Ui

	filePath string
}

// New is the constructor of MessageCommand
func New(ui cli.Ui) *MessageCommand {
	return &MessageCommand{
		UI: ui,
	}
}

// Help implements the cli.Command interface
func (fc *MessageCommand) Help() string {
	return `Runs the message and timeouts replay for analysis and testing purposes based on provided .flow file.
	
	Usage: replay-messages -file={fullPathToFlowFile}
	
	Options:
	
	-file - Full path to .flow file containing messages and timeouts to be replayed by the fuzz framework`
}

// Synopsis implements the cli.Command interface
func (rmc *MessageCommand) Synopsis() string {
	return "Starts the replay of messages and timeouts in fuzz runner"
}

// Run implements the cli.Command interface and runs the command
func (rmc *MessageCommand) Run(args []string) int {
	err := rmc.validateInput(args)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	messageReader := replay.NewMessageReader()

	err = messageReader.OpenFile(rmc.filePath)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	nodeNames, err := messageReader.ReadNodeMetaData()
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	i := strings.Index(nodeNames[0], "_")
	prefix := ""
	if i > -1 {
		prefix = (nodeNames[0])[:i]
	}

	replayMessagesNotifier := replay.NewMessagesNotifierWithReader(messageReader)

	nodesCount := len(nodeNames)
	config := &e2e.ClusterConfig{
		Count:                 nodesCount,
		Name:                  "fuzz_cluster",
		Prefix:                prefix,
		ReplayMessageNotifier: replayMessagesNotifier,
		RoundTimeout:          e2e.GetPredefinedTimeout(time.Millisecond),
		TransportHandler:      func(to pbft.NodeID, msg *pbft.MessageReq) { replayMessagesNotifier.HandleMessage(to, msg) },
		CreateBackend: func() e2e.IntegrationBackend {
			return replay.NewBackend(messageReader)
		},
	}

	cluster := e2e.NewPBFTCluster(nil, config)

	messageReader.ReadMessages(cluster)
	messageReader.CloseFile()

	var wg sync.WaitGroup
	wg.Add(1)

	nodesDone := make(map[string]bool, nodesCount)
	go func() {
		doneChan := messageReader.ProcessingDone()
		for {
			nodeDone := <-doneChan
			nodesDone[nodeDone] = true
			cluster.StopNode(nodeDone)
			if len(nodesDone) == nodesCount {
				wg.Done()
				return
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
func (rmc *MessageCommand) NewFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet("replay-messages", flag.ContinueOnError)
	flagSet.StringVar(&rmc.filePath, "file", "", "Full path to .flow file containing messages and timeouts to be replayed by the fuzz framework")

	return flagSet
}

// validateInput parses arguments from CLI and validates their correctness
func (rmc *MessageCommand) validateInput(args []string) error {
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
