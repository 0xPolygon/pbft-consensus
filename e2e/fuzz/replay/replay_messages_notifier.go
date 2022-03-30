package replay

import (
	"github.com/0xPolygon/pbft-consensus"
)

const (
	FileName = "messages"
)

// ReplayMessagesNotifier is a struct that implements ReplayNotifier interface
type ReplayMessagesNotifier struct {
	messagePersister *replayMessagePersister
	messageReader    *replayMessageReader
}

// NewReplayMessagesNotifierWithPersister creates a new messages notifier with messages persister (required when fuzz-run is executed to save messages to file)
func NewReplayMessagesNotifierWithPersister() *ReplayMessagesNotifier {
	return &ReplayMessagesNotifier{
		messagePersister: &replayMessagePersister{},
	}
}

// NewReplayMessagesNotifierWithReader creates a new messages notifier with messages reader (required when replay-messages is executed to read messages from file)
func NewReplayMessagesNotifierWithReader(r *replayMessageReader) *ReplayMessagesNotifier {
	return &ReplayMessagesNotifier{
		messageReader:    r,
		messagePersister: &replayMessagePersister{},
	}
}

// SaveMetaData saves node meta data to .flow file
func (r *ReplayMessagesNotifier) SaveMetaData(nodeNames *[]string) error {
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
func (r *ReplayMessagesNotifier) ReadNextMessage(p *pbft.Pbft) (*pbft.MessageReq, []*pbft.MessageReq) {
	msg, discards := p.ReadMessageWithDiscards()

	if r.messageReader != nil && msg != nil && r.messageReader.processMessage(p.GetValidatorId(), msg) {
		return nil, nil
	}

	return msg, discards
}

// CloseFile closes file created by the ReplayMessagesHandler if it is open
func (r *ReplayMessagesNotifier) CloseFile() error {
	return r.messagePersister.closeFile()
}
