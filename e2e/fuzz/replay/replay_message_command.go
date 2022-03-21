package replay

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
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
	err := rmc.validateInput(args)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	file, err := openFile(rmc.filePath)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	scanner := createScanner(file)
	nodeNames, err := readNodeMetaData(scanner)
	if err != nil {
		rmc.UI.Error(err.Error())
		return 1
	}

	nodesCount := len(nodeNames)
	messagesChannel := make(chan []*ReplayMessage)
	doneChannel := make(chan struct{})

	go func(s *bufio.Scanner) {
		i := 0
		messages := make([]*ReplayMessage, 0)
		for scanner.Scan() {
			var message *ReplayMessage
			if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
				rmc.UI.Error(fmt.Sprintf("Error happened on unmarshalling a message in .flow file. Reason: %v", err))
				return
			}
			messages = append(messages, message)
			i++

			if i%1000 == 0 {
				messagesChannel <- messages
				messages = nil
			}
		}
		doneChannel <- struct{}{}
	}(scanner)

	i := strings.Index(nodeNames[0], "_")
	prefix := ""
	if i > -1 {
		prefix = (nodeNames[0])[:i]
	}

	replayMessagesNotifier := NewReplayMessagesNotifier(nodesCount)
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

	isDone := false
L:
	for !isDone {
		select {
		case messages := <-messagesChannel:
			for _, message := range messages {
				node, exists := nodes[string(message.To)]
				if !exists {
					rmc.UI.Warn(fmt.Sprintf("Could not find node: %v to push message from .flow file", message.To))
				} else if message.To != message.Message.From || message.Message.Type == pbft.MessageReq_Preprepare {
					// since timeouts have .From property always empty, and To always non empty, they will be pushed to message queue
					// for regular messages we will only push those where sender and receiver differ or if its a PrePrepare message
					node.PushMessageInternal(message.Message)
				}
			}
		case <-doneChannel:
			isDone = true
			break L
		}
	}

	file.Close()

	var wg sync.WaitGroup
	wg.Add(nodesCount)

	for i := 0; i < nodesCount; i++ {
		go func() {
			defer wg.Done()
			<-replayMessagesNotifier.queueDrainChannel
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

// openFile opens the file on provided location
func openFile(filePath string) (*os.File, error) {
	_, err := os.Stat(filePath)
	if err != nil {
		return nil, err
	}

	return os.Open(filePath)
}

// createScanner creates a new bufio Scanner to load file line by line
func createScanner(file *os.File) *bufio.Scanner {
	scanner := bufio.NewScanner(file)

	buffer := []byte{}
	scanner.Buffer(buffer, MaxCharactersPerLine)

	return scanner
}

// readNodeMetaData reads the first line of .flow file which should be a list of nodes
func readNodeMetaData(scanner *bufio.Scanner) ([]string, error) {
	var nodeNames []string
	scanner.Scan() // first line carries the node names needed to create appropriate number of nodes for replay
	err := json.Unmarshal(scanner.Bytes(), &nodeNames)
	if err != nil {
		return nil, err
	} else if len(nodeNames) == 0 {
		err = errors.New("no nodes were found in .flow file, so no cluster will be started")
	}

	return nodeNames, err
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
