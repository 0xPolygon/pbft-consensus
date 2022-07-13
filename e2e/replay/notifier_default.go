package replay

import "github.com/0xPolygon/pbft-consensus"

// DefaultReplayNotifier is a null object implementation of ReplayNotifier interface
type DefaultReplayNotifier struct {
}

// HandleTimeout implements StateNotifier interface
func (n *DefaultReplayNotifier) HandleTimeout(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) {
}

// ReadNextMessage is an implementation of StateNotifier interface
func (n *DefaultReplayNotifier) ReadNextMessage(p *pbft.Pbft) (*pbft.MessageReq, []*pbft.MessageReq) {
	return p.ReadMessageWithDiscards()
}

// SaveMetaData is an implementation of ReplayNotifier interface
func (n *DefaultReplayNotifier) SaveMetaData(nodeNames *[]string) error { return nil }

// SaveState is an implementation of ReplayNotifier interface
func (n *DefaultReplayNotifier) SaveState() error { return nil }

// HandleMessage is an implementation of ReplayNotifier interface
func (n *DefaultReplayNotifier) HandleMessage(to pbft.NodeID, message *pbft.MessageReq) {}
