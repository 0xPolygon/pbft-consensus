package replay

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

const (
	MaxCharactersPerLine   = 2048 * 1024 // Increase Scanner buffer size to 2MB per line
	roundMessagesWaitLimit = 50
	messageChunkSize       = 200
)

// replayMessageReader encapsulates logic for reading messages from flow file
type replayMessageReader struct {
	lock                   sync.Mutex
	file                   *os.File
	scanner                *bufio.Scanner
	lastSequence           uint64
	msgProcessingDone      chan string
	nodesDoneWithExecution map[pbft.NodeID]bool
}

// openFile opens the file on provided location
func (r *replayMessageReader) openFile(filePath string) error {
	_, err := os.Stat(filePath)
	if err != nil {
		return err
	}

	r.file, err = os.Open(filePath)
	if err != nil {
		return err
	}

	r.scanner = bufio.NewScanner(r.file)

	buffer := []byte{}
	r.scanner.Buffer(buffer, MaxCharactersPerLine)

	return nil
}

// closeFile closes the opened .flow file
func (r *replayMessageReader) closeFile() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// readNodeMetaData reads the first line of .flow file which should be a list of nodes
func (r *replayMessageReader) readNodeMetaData() ([]string, error) {
	var nodeNames []string
	r.scanner.Scan() // first line carries the node names needed to create appropriate number of nodes for replay
	err := json.Unmarshal(r.scanner.Bytes(), &nodeNames)
	if err != nil {
		return nil, err
	} else if len(nodeNames) == 0 {
		err = errors.New("no nodes were found in .flow file, so no cluster will be started")
	}

	return nodeNames, err
}

// readMessages reads messages from open .flow file and pushes them to appropriate nodes
func (r *replayMessageReader) readMessages(cluster *e2e.Cluster) {
	nodes := cluster.GetNodesMap()
	r.nodesDoneWithExecution = make(map[pbft.NodeID]bool, len(nodes))

	messagesChannel := make(chan []*ReplayMessage)
	doneChannel := make(chan struct{})

	r.startChunkReading(messagesChannel, doneChannel)

	isDone := false
LOOP:
	for !isDone {
		select {
		case messages := <-messagesChannel:
			for _, message := range messages {
				node, exists := nodes[string(message.To)]
				if !exists {
					log.Println(fmt.Sprintf("[WARNING] Could not find node: %v to push message from .flow file", message.To))
				} else {
					node.PushMessageInternal(message.Message)
					if r.lastSequence < message.Message.View.Sequence {
						r.lastSequence = message.Message.View.Sequence
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
		messages := make([]*ReplayMessage, 0)
		i := 0
		for r.scanner.Scan() {
			var message *ReplayMessage
			if err := json.Unmarshal(r.scanner.Bytes(), &message); err != nil {
				log.Println((fmt.Sprintf("[ERROR] Error happened on unmarshalling a message in .flow file. Reason: %v", err)))
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

// checks if given node is done with execution of messages
func (r *replayMessageReader) checkIfDoneWithExecution(validatorId pbft.NodeID, msg *pbft.MessageReq) {
	//when nodes reach sequence that is higher than the sequence in file,
	//or if it is in constant round change we know we either reached the end of execution or nodes are stuck and there is a problem
	if msg.View.Sequence > r.lastSequence ||
		isSequenceInContinuousRoundChange(msg, validatorId) {
		r.lock.Lock()
		if _, isDone := r.nodesDoneWithExecution[validatorId]; !isDone {
			r.nodesDoneWithExecution[validatorId] = true
			r.msgProcessingDone <- string(validatorId)
		}
		r.lock.Unlock()
	}
}

// isTimeoutMessage checks if message in .flow file represents a timeout
func isTimeoutMessage(message *pbft.MessageReq) bool {
	return message.Hash == nil && message.Proposal == nil && message.Seal == nil && message.From == ""
}

// isSequenceInContinuousRoundChange checks if node is stuck in continuous round change
// means either all messages are read and processed from file and nodes can not reach a consensus for next proposal, or there is some problem
func isSequenceInContinuousRoundChange(msg *pbft.MessageReq, validatorId pbft.NodeID) bool {
	return msg.Type == pbft.MessageReq_RoundChange && msg.View.Round >= roundMessagesWaitLimit
}
