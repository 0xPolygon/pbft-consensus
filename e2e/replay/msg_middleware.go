package replay

import (
	"log"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e/helper"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

const (
	FileName = "messages"
)

// MessagesMiddleware is a struct that implements Notifier interface
type MessagesMiddleware struct {
	messagePersister *messagePersister
	messageReader    *MessageReader
}

// NewMessagesMiddlewareWithPersister creates a new messages notifier with messages persister (required when fuzz-run is executed to save messages to file)
func NewMessagesMiddlewareWithPersister() *MessagesMiddleware {
	return &MessagesMiddleware{
		messagePersister: &messagePersister{},
	}
}

// NewMessagesMiddlewareWithReader creates a new messages notifier with messages reader (required when replay-messages is executed to read messages from file)
func NewMessagesMiddlewareWithReader(r *MessageReader) *MessagesMiddleware {
	return &MessagesMiddleware{
		messageReader:    r,
		messagePersister: &messagePersister{},
	}
}

// SaveMetaData saves node meta data to .flow file
func (r *MessagesMiddleware) SaveMetaData(nodeNames *[]string) error {
	return r.messagePersister.saveMetaData(nodeNames)
}

// SaveState saves currently cached messages and timeouts to .flow file
func (r *MessagesMiddleware) SaveState() error {
	return r.messagePersister.saveCachedMessages()
}

// HandleMessage caches processed message to be saved later in .flow file
func (r *MessagesMiddleware) HandleMessage(to pbft.NodeID, message *pbft.MessageReq) {
	r.messagePersister.addMessage(newMessage(to, message))
}

// HandleTimeout is an implementation of StateNotifier interface
func (r *MessagesMiddleware) HandleTimeout(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) {
	r.messagePersister.addMessage(newTimeoutMessage(to, msgType, view))
}

// ReadNextMessage is an implementation of StateNotifier interface
func (r *MessagesMiddleware) ReadNextMessage(p *pbft.Pbft) (*pbft.MessageReq, []*pbft.MessageReq) {
	msg, discards := p.ReadMessageWithDiscards()

	if r.messageReader != nil && msg != nil {
		if isTimeoutMessage(msg) {
			return nil, nil
		} else {
			r.messageReader.checkIfDoneWithExecution(p.GetValidatorId(), msg)
		}
	}

	return msg, discards
}

// CloseFile closes file created by the ReplayMessagesHandler if it is open
func (r *MessagesMiddleware) CloseFile() error {
	return r.messagePersister.closeFile()
}

// Backend implements the IntegrationBackend interface and implements its own BuildProposal method for replay
type Backend struct {
	e2e.Fsm
	messageReader *MessageReader
}

// NewBackend is the constructor of Backend
func NewBackend(messageReader *MessageReader) *Backend {
	return &Backend{
		messageReader: messageReader,
	}
}

// BuildProposal builds the next proposal. If it has a preprepare message for given height in .flow file it will take the proposal from file, otherwise it will generate a new one
func (f *Backend) BuildProposal() (*pbft.Proposal, error) {
	var data []byte
	sequence := f.Height()
	if prePrepareMessage, exists := f.messageReader.prePrepareMessages[sequence]; exists && prePrepareMessage != nil {
		data = prePrepareMessage.Proposal
	} else {
		log.Printf("[WARNING] Could not find PRE-PREPARE message for sequence: %v", sequence)
		data = helper.GenerateProposal()
	}

	return &pbft.Proposal{
		Data: data,
		Time: time.Now().Add(1 * time.Second),
		Hash: helper.Hash(data),
	}, nil
}
