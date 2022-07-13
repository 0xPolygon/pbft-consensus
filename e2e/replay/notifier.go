package replay

import "github.com/0xPolygon/pbft-consensus"

// Notifier is an interface that expands the StateNotifier with additional methods for saving and loading replay messages
type Notifier interface {
	pbft.StateNotifier
	SaveMetaData(nodeNames *[]string) error
	SaveState() error
	HandleMessage(to pbft.NodeID, message *pbft.MessageReq)
}
