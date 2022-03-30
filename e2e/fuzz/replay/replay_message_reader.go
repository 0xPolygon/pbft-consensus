package replay

import (
	"bufio"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

const (
	maxCharactersPerLine = 2048 * 1024 // Increase Scanner buffer size to 2MB per line
	messageChunkSize     = 200
)

type sequenceMessages struct {
	sequence uint64
	messages []*pbft.MessageReq
}

// replayMessageReader encapsulates logic for reading messages from flow file
type replayMessageReader struct {
	lock                   sync.Mutex
	file                   *os.File
	scanner                *bufio.Scanner
	msgProcessingDone      chan string
	nodesDoneWithExecution map[pbft.NodeID]bool
	lastSequenceMessages   map[pbft.NodeID]*sequenceMessages
	prePrepareMessages     map[uint64]*pbft.MessageReq
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
	r.scanner.Buffer(buffer, maxCharactersPerLine)

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

	nodesCount := len(nodes)
	r.nodesDoneWithExecution = make(map[pbft.NodeID]bool, nodesCount)
	r.lastSequenceMessages = make(map[pbft.NodeID]*sequenceMessages, nodesCount)
	r.prePrepareMessages = make(map[uint64]*pbft.MessageReq)

	messagesChannel := make(chan []*ReplayMessage)
	doneChannel := make(chan struct{})

	r.startChunkReading(messagesChannel, doneChannel)
	nodeMessages := make(map[pbft.NodeID]map[uint64][]*pbft.MessageReq, nodesCount)
	for _, n := range nodes {
		nodeMessages[pbft.NodeID(n.GetName())] = make(map[uint64][]*pbft.MessageReq)
	}

	isDone := false
LOOP:
	for !isDone {
		select {
		case messages := <-messagesChannel:
			for _, message := range messages {
				node, exists := nodes[string(message.To)]
				if !exists {
					log.Printf("[WARNING] Could not find node: %v to push message from .flow file.\n", message.To)
				} else {
					node.PushMessageInternal(message.Message)
					nodeMessages[message.To][message.Message.View.Sequence] = append(nodeMessages[message.To][message.Message.View.Sequence], message.Message)

					if !isTimeoutMessage(message.Message) && message.Message.Type == pbft.MessageReq_Preprepare {
						if _, isPrePrepareAdded := r.prePrepareMessages[message.Message.View.Sequence]; !isPrePrepareAdded {
							r.prePrepareMessages[message.Message.View.Sequence] = message.Message
						}
					}
				}
			}
		case <-doneChannel:
			for name, n := range nodeMessages {
				nodeLastSequence := uint64(0)
				for sequence := range n {
					if nodeLastSequence < sequence {
						nodeLastSequence = sequence
					}
				}

				r.lastSequenceMessages[name] = &sequenceMessages{
					sequence: nodeLastSequence,
					messages: nodeMessages[name][nodeLastSequence],
				}
			}

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
				log.Printf("[ERROR] Error happened on unmarshalling a message in .flow file. Reason: %v.\n", err)
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

// checkIfDoneWithExecution checks if node finished with processing all the messages from .flow file
func (r *replayMessageReader) checkIfDoneWithExecution(validatorId pbft.NodeID, msg *pbft.MessageReq) {
	if msg.View.Sequence > r.lastSequenceMessages[validatorId].sequence ||
		(msg.View.Sequence == r.lastSequenceMessages[validatorId].sequence && r.areMessagesFromLastSequenceProcessed(msg, validatorId)) {
		r.lock.Lock()
		if _, isDone := r.nodesDoneWithExecution[validatorId]; !isDone {
			r.nodesDoneWithExecution[validatorId] = true
			r.msgProcessingDone <- string(validatorId)
		}
		r.lock.Unlock()
	}
}

// areMessagesFromLastSequenceProcessed checks if all the messages from the last sequence of given node are processed so that the node can be stoped
func (r *replayMessageReader) areMessagesFromLastSequenceProcessed(msg *pbft.MessageReq, validatorId pbft.NodeID) bool {
	lastSequenceMessages := r.lastSequenceMessages[validatorId]

	lastSequenceMessagesCount := len(lastSequenceMessages.messages)
	if lastSequenceMessagesCount > 0 {
		messageIndexToRemove := -1
		for i, message := range lastSequenceMessages.messages {
			if msg.Equal(message) {
				messageIndexToRemove = i
				break
			}
		}

		if messageIndexToRemove != -1 {
			lastSequenceMessages.messages = append(lastSequenceMessages.messages[:messageIndexToRemove], lastSequenceMessages.messages[messageIndexToRemove+1:]...)
			lastSequenceMessagesCount = len(lastSequenceMessages.messages)
		}
	}

	return lastSequenceMessagesCount == 0
}

// isTimeoutMessage checks if message in .flow file represents a timeout
func isTimeoutMessage(message *pbft.MessageReq) bool {
	return message.Hash == nil && message.Proposal == nil && message.Seal == nil && message.From == ""
}
