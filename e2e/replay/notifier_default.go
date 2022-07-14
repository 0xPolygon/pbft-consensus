package replay

import "github.com/0xPolygon/pbft-consensus"

// DefaultNotifier is a null object implementation of ReplayNotifier interface
type DefaultNotifier struct {
}

// HandleTimeout implements StateNotifier interface
func (n *DefaultNotifier) HandleTimeout(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) {
}

// ReadNextMessage is an implementation of StateNotifier interface
func (n *DefaultNotifier) ReadNextMessage(p *pbft.Pbft) (*pbft.MessageReq, []*pbft.MessageReq) {
	return p.ReadMessageWithDiscards()
}

// SaveMetaData is an implementation of ReplayNotifier interface
func (n *DefaultNotifier) SaveMetaData(nodeNames *[]string) error { return nil }

// SaveState is an implementation of ReplayNotifier interface
func (n *DefaultNotifier) SaveState() error { return nil }

// HandleMessage is an implementation of ReplayNotifier interface
func (n *DefaultNotifier) HandleMessage(to pbft.NodeID, message *pbft.MessageReq) {}
