package replay

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

const (
	maxCharactersPerLine = 2048 * 1024 // Increase Scanner buffer size to 2MB per line
	messageChunkSize     = 200         // Size of a chunk of messages being loaded from .flow file
)

// replayMessageReader encapsulates logic for reading messages from flow file
type replayMessageReader struct {
	messagesFile       *os.File
	metaDataFile       *os.File
	prePrepareMessages map[uint64]*pbft.MessageReq
}

// openFiles opens the .flow files on provided location
func (r *replayMessageReader) openFiles(filesDirectory string) error {
	_, err := os.Stat(filesDirectory)
	if err != nil {
		return err
	}

	messagesFilePath := filepath.Join(filesDirectory, fmt.Sprintf("%v.flow", MessagesFilePrefix))
	_, err = os.Stat(messagesFilePath)
	if err != nil {
		return err
	}

	r.messagesFile, err = os.Open(messagesFilePath)
	if err != nil {
		return err
	}

	metaDataFilePath := filepath.Join(filesDirectory, fmt.Sprintf("%v.flow", MetaDataFilePrefix))
	_, err = os.Stat(metaDataFilePath)
	if err != nil {
		return err
	}

	r.metaDataFile, err = os.Open(metaDataFilePath)
	if err != nil {
		return err
	}

	return nil
}

// closeFile closes the opened .flow files
func (r *replayMessageReader) closeFile() error {
	var errors []string
	if r.messagesFile != nil {
		if err := r.messagesFile.Close(); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if r.metaDataFile != nil {
		if err := r.metaDataFile.Close(); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf(strings.Join(errors, "\n"))
	}

	return nil
}

// readNodeMetaData reads the node names and action from metaData.flow file
func (r *replayMessageReader) readNodeMetaData(nodeExecutionHandler *replayNodeExecutionHandler) ([]string, error) {
	scanner := bufio.NewScanner(r.metaDataFile)
	buffer := []byte{}
	scanner.Buffer(buffer, maxCharactersPerLine)

	var nodeNames []string
	scanner.Scan() // first line carries the node names needed to create appropriate number of nodes for replay
	err := json.Unmarshal(scanner.Bytes(), &nodeNames)
	nodeCount := len(nodeNames)
	if err != nil {
		return nil, err
	} else if nodeCount == 0 {
		err = errors.New("no nodes were found in .flow file, so no cluster will be started")
		return nil, err
	}

	nodeExecutionHandler.dropNodeActions = make(map[pbft.NodeID]map[uint64]*MetaData, nodeCount)
	nodeExecutionHandler.revertDropNodeActions = make(map[pbft.NodeID]map[uint64]*MetaData, nodeCount)
	nodeExecutionHandler.lastSequencesByNode = make(map[pbft.NodeID]*MetaData, nodeCount)

	for _, name := range nodeNames {
		nodeExecutionHandler.dropNodeActions[pbft.NodeID(name)] = make(map[uint64]*MetaData)
		nodeExecutionHandler.revertDropNodeActions[pbft.NodeID(name)] = make(map[uint64]*MetaData)
	}

	for scanner.Scan() {
		var action *MetaData
		if err := json.Unmarshal(scanner.Bytes(), &action); err != nil {
			log.Printf("[ERROR] Error happened on unmarshalling action in .flow file. Reason: %v", err)
			return nodeNames, err
		}

		switch action.DataType {
		case e2e.DropNode:
			nodeExecutionHandler.dropNodeActions[pbft.NodeID(action.Data)][action.Sequence] = action
		case e2e.RevertDropNode:
			nodeExecutionHandler.revertDropNodeActions[pbft.NodeID(action.Data)][action.Sequence] = action
		case e2e.LastSequence:
			nodeExecutionHandler.lastSequencesByNode[pbft.NodeID(action.Data)] = action
		default:
			panic("unknown action data type read from metaData.flow file")
		}
	}

	return nodeNames, err
}

// readMessages reads messages from open .flow file and pushes them to appropriate nodes
func (r *replayMessageReader) readMessages(cluster *e2e.Cluster, nodeExecutionHandler *replayNodeExecutionHandler) {
	nodes := cluster.GetNodesMap()

	messagesChannel := make(chan []*ReplayMessage)
	doneChannel := make(chan struct{})
	r.prePrepareMessages = make(map[uint64]*pbft.MessageReq)

	r.startChunkReading(messagesChannel, doneChannel)

	isDone := false
LOOP:
	for !isDone {
		select {
		case messages := <-messagesChannel:
			for _, message := range messages {
				node, exists := nodes[string(message.To)]
				if !exists {
					log.Printf("[WARNING] Could not find node: %v to push message from .flow file", message.To)
				} else {
					node.PushMessageInternal(message.Message)
					if !isTimeoutMessage(message.Message) && message.Message.Type == pbft.MessageReq_Preprepare {
						if _, isPrePrepareAdded := r.prePrepareMessages[message.Message.View.Sequence]; !isPrePrepareAdded {
							r.prePrepareMessages[message.Message.View.Sequence] = message.Message
						}
					}
				}
			}
		case <-doneChannel:
			isDone = true
			break LOOP
		}
	}
}

// startChunkReading reads messages from .flow file in chunks
func (r *replayMessageReader) startChunkReading(messagesChannel chan []*ReplayMessage, doneChannel chan struct{}) {
	go func() {
		scanner := bufio.NewScanner(r.messagesFile)
		buffer := []byte{}
		scanner.Buffer(buffer, maxCharactersPerLine)

		messages := make([]*ReplayMessage, 0)
		i := 0
		for scanner.Scan() {
			var message *ReplayMessage
			if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
				log.Printf("[ERROR] Error happened on unmarshalling a message in .flow file. Reason: %v", err)
				return
			}

			messages = append(messages, message)
			i++

			if i%messageChunkSize == 0 {
				messagesChannel <- messages
				messages = nil
			}
		}

		if len(messages) > 0 {
			//its the leftover of messages
			messagesChannel <- messages
			messages = nil
		}

		doneChannel <- struct{}{}
	}()
}
