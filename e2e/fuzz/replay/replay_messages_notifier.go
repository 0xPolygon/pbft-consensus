package replay

import (
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

const (
	MessagesFilePrefix = "messages"
	MetaDataFilePrefix = "metaData"
)

// ReplayMessagesNotifier is a struct that implements ReplayNotifier interface
type ReplayMessagesNotifier struct {
	messagePersister    messagePersister
	nodeExectionHandler *replayNodeExecutionHandler
}

// NewReplayMessagesNotifierWithPersister creates a new messages notifier with messages persister (required when fuzz-run is executed to save messages to file)
func NewReplayMessagesNotifierWithPersister() *ReplayMessagesNotifier {
	return &ReplayMessagesNotifier{
		messagePersister: &replayMessagePersister{},
	}
}

// NewReplayMessagesNotifierForReplay creates a new messages notifier with plugins needed for replay
func NewReplayMessagesNotifierForReplay(nodeExecutionHandler *replayNodeExecutionHandler) *ReplayMessagesNotifier {
	return &ReplayMessagesNotifier{
		messagePersister:    &defaultMessagePersister{},
		nodeExectionHandler: nodeExecutionHandler,
	}
}

// SaveMetaData saves node meta data to .flow file
func (r *ReplayMessagesNotifier) SaveMetaData(nodeNames []string) error {
	return r.messagePersister.saveMetaData(nodeNames)
}

// SaveState saves currently cached messages and timeouts to .flow file
func (r *ReplayMessagesNotifier) SaveState() error {
	return r.messagePersister.saveCachedMessages()
}

// HandleMessage caches processed message to be saved later in .flow file
func (r *ReplayMessagesNotifier) HandleMessage(to pbft.NodeID, message *pbft.MessageReq) {
	r.messagePersister.addMessage(NewReplayMessageReq(to, message))
}

// HandleTimeout is an implementation of StateNotifier interface
func (r *ReplayMessagesNotifier) HandleTimeout(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) {
	r.messagePersister.addMessage(NewReplayTimeoutMessage(to, msgType, view))
}

// ReadNextMessage is an implementation of StateNotifier interface
func (r *ReplayMessagesNotifier) ReadNextMessage(p *pbft.Pbft, timeoutChannel chan time.Time) (*pbft.MessageReq, []*pbft.MessageReq) {
	msg, discards := p.ReadMessageWithDiscards()

	if r.nodeExectionHandler != nil {
		validatorId := p.GetValidatorId()
		if msg != nil {
			r.nodeExectionHandler.checkForNodesToBeRestarted(msg.View.Sequence)
			if r.nodeExectionHandler.checkIsTimeout(validatorId, msg, timeoutChannel) {
				return nil, nil
			}
		} else {
			// if node has no more messages for given round and sequence in queue it was dropped in fuzz
			r.nodeExectionHandler.checkIfShouldDrop(validatorId, p.GetCurrentView(), timeoutChannel)
		}
	}

	return msg, discards
}

// CloseFile closes file created by the ReplayMessagesHandler if it is open
func (r *ReplayMessagesNotifier) CloseFile() error {
	return r.messagePersister.closeFile()
}

// CreateTimeoutChannel is an implementation of StateNotifier interface
func (r *ReplayMessagesNotifier) CreateTimeoutChannel(timeout time.Duration) chan time.Time {
	return make(chan time.Time)
}

// HandleAction is an implementation of ReplayMessageNotifier interface
func (r *ReplayMessagesNotifier) HandleAction(actionType, data string, sequence, round uint64) {
	r.messagePersister.addAction(NewMetaData(actionType, data, sequence, round))
}
