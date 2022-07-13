package replay

import (
	"github.com/0xPolygon/pbft-consensus"
)

// message is a struct that represents a single json object in .flow file
type message struct {
	To      pbft.NodeID      `json:"to"`
	Message *pbft.MessageReq `json:"message"`
}

// newMessage creates a new message to be written to .flow file
func newMessage(to pbft.NodeID, messageReq *pbft.MessageReq) *message {
	return &message{
		To:      to,
		Message: messageReq,
	}
}

// newTimeoutMessage creates a new timeout to be written to .flow file
func newTimeoutMessage(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) *message {
	return &message{
		To: to,
		Message: &pbft.MessageReq{
			Type: msgType,
			View: view,
		},
	}
}
