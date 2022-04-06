package replay

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/mitchellh/cli"
)

// ReplayMessageCommand is a struct containing data for running replay-message command
type ReplayMessageCommand struct {
	UI cli.Ui

	filesDirectory string
}

// Help implements the cli.Command interface
func (rmc *ReplayMessageCommand) Help() string {
	return `Runs the message and timeouts replay for analysis and testing purposes based on provided .flow file.
	
	Usage: replay-messages -messagesFile={fullPathToFlowFile} -metaDataFile={fullPathToFlowFile}
	
	Options:
	
	-filesDirectory - Directory containing .flow files for messages and actions meta data to be replayed by the fuzz framework`
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

	messageReader := &replayMessageReader{}
	nodeExecutionHandler := NewNodeExecutionHandler()

	err = messageReader.openFiles(rmc.filesDirectory)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	nodeNames, err := messageReader.readNodeMetaData(nodeExecutionHandler)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	i := strings.Index(nodeNames[0], "_")
	prefix := ""
	if i > -1 {
		prefix = (nodeNames[0])[:i]
	}

	replayMessagesNotifier := NewReplayMessagesNotifierForReplay(nodeExecutionHandler)

	nodesCount := len(nodeNames)
	var cluster *e2e.Cluster
	config := &e2e.ClusterConfig{
		Count:                 nodesCount,
		Name:                  "fuzz_cluster",
		Prefix:                prefix,
		ReplayMessageNotifier: replayMessagesNotifier,
		RoundTimeout:          e2e.GetPredefinedTimeout(time.Millisecond),
		TransportHandler:      func(to pbft.NodeID, msg *pbft.MessageReq) { /* we do not gossip messages in replay */ },
		CreateBackend:         func() e2e.IntegrationBackend { return &ReplayBackend{messageReader: messageReader} },
	}

	cluster = e2e.NewPBFTCluster(nil, config)

	messageReader.readMessages(cluster, nodeExecutionHandler)
	messageReader.closeFile()

	nodeExecutionHandler.startActionSimulation(cluster)
	nodeExecutionHandler.stopActionSimulation(cluster)

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
	flagSet.StringVar(&rmc.filesDirectory, "filesDirectory", "", "Directory containing .flow files for messages and actions meta data to be replayed by the fuzz framework")

	return flagSet
}

// validateInput parses arguments from CLI and validates their correctness
func (rmc *ReplayMessageCommand) validateInput(args []string) error {
	flagSet := rmc.NewFlagSet()
	err := flagSet.Parse(args)
	if err != nil {
		return err
	}

	if rmc.filesDirectory == "" {
		err = errors.New("provided files directory is empty")
		return err
	}

	return nil
}

// ReplayBackend implements the IntegrationBackend interface and implements its own BuildProposal method for replay
type ReplayBackend struct {
	e2e.Fsm
	messageReader *replayMessageReader
}

// BuildProposal builds the next proposal. If it has a preprepare message for given height in .flow file it will take the proposal from file, otherwise it will generate a new one
func (f *ReplayBackend) BuildProposal() (*pbft.Proposal, error) {
	var data []byte
	sequence := f.Height()
	if prePrepareMessage, exists := f.messageReader.prePrepareMessages[sequence]; exists && prePrepareMessage != nil {
		data = prePrepareMessage.Proposal
	} else {
		log.Printf("[WARNING] Could not find PRE-PREPARE message for sequence: %v", sequence)
		data = e2e.GenerateProposal()
	}

	return &pbft.Proposal{
		Data: data,
		Time: time.Now().Add(1 * time.Second),
		Hash: e2e.Hash(data),
	}, nil
}
