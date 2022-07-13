package replay

import (
	"errors"
	"flag"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e/helper"

	"github.com/mitchellh/cli"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/0xPolygon/pbft-consensus/e2e/replay"
)

// Command is a struct containing data for running replay-message command
type Command struct {
	UI cli.Ui

	filePath string
}

// New is the constructor of Command
func New(ui cli.Ui) *Command {
	return &Command{
		UI: ui,
	}
}

// Help implements the cli.Command interface
func (c *Command) Help() string {
	return `Runs the message and timeouts replay for analysis and testing purposes based on provided .flow file.
	
	Usage: replay-messages -file={fullPathToFlowFile}
	
	Options:
	
	-file - Full path to .flow file containing messages and timeouts to be replayed by the fuzz framework`
}

// Synopsis implements the cli.Command interface
func (c *Command) Synopsis() string {
	return "Starts the replay of messages and timeouts in fuzz runner"
}

// Run implements the cli.Command interface and runs the command
func (c *Command) Run(args []string) int {
	err := c.validateInput(args)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	messageReader := replay.NewMessageReader()

	err = messageReader.OpenFile(c.filePath)
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	nodeNames, err := messageReader.ReadNodeMetaData()
	if err != nil {
		c.UI.Error(err.Error())
		return 1
	}

	i := strings.Index(nodeNames[0], "_")
	prefix := ""
	if i > -1 {
		prefix = (nodeNames[0])[:i]
	}

	replayMessagesNotifier := replay.NewMessagesMiddlewareWithReader(messageReader)

	nodesCount := len(nodeNames)
	config := &e2e.ClusterConfig{
		Count:                 nodesCount,
		Name:                  "fuzz_cluster",
		Prefix:                prefix,
		ReplayMessageNotifier: replayMessagesNotifier,
		RoundTimeout:          helper.GetPredefinedTimeout(time.Millisecond),
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

	c.UI.Info("Done with execution")
	if err = replayMessagesNotifier.CloseFile(); err != nil {
		c.UI.Error(fmt.Sprintf("Error while closing .flow file: '%s'\n", err))
		return 1
	}

	return 0
}

// NewFlagSet implements the FuzzCLICommand interface and creates a new flag set for command arguments
func (c *Command) NewFlagSet() *flag.FlagSet {
	flagSet := flag.NewFlagSet("replay-messages", flag.ContinueOnError)
	flagSet.StringVar(&c.filePath, "file", "", "Full path to .flow file containing messages and timeouts to be replayed by the fuzz framework")

	return flagSet
}

// validateInput parses arguments from CLI and validates their correctness
func (c *Command) validateInput(args []string) error {
	flagSet := c.NewFlagSet()
	err := flagSet.Parse(args)
	if err != nil {
		return err
	}

	if c.filePath == "" {
		err = errors.New("provided file path is empty")
		return err
	}
	return nil
}
