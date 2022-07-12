package replay

import (
	"encoding/json"

	"github.com/0xPolygon/pbft-consensus"
)

// Message is a struct that represents a single json object in .flow file
type Message struct {
	To      pbft.NodeID      `json:"to"`
	Message *pbft.MessageReq `json:"message"`
}

// NewMessageReq creates a new message to be written to .flow file
func NewMessageReq(to pbft.NodeID, message *pbft.MessageReq) *Message {
	return &Message{
		To:      to,
		Message: message,
	}
}

// NewTimeoutMessage creates a new timeout to be written to .flow file
func NewTimeoutMessage(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) *Message {
	return &Message{
		To: to,
		Message: &pbft.MessageReq{
			Type: msgType,
			View: view,
		},
	}
}

// ConvertToByteArrays converts ReplayMessage slice to JSON representation and return it back as slice of byte arrays
func ConvertToByteArrays(messages []*Message) ([][]byte, error) {
	var allRawMessages [][]byte
	for _, message := range messages {
		currentRawMessage, err := json.Marshal(message)
		if err != nil {
			return allRawMessages, err
		}
		allRawMessages = append(allRawMessages, currentRawMessage)
	}
	return allRawMessages, nil
}

// isTimeoutMessage checks if message in .flow file represents a timeout
func isTimeoutMessage(message *pbft.MessageReq) bool {
	return message.Hash == nil && message.Proposal == nil && message.Seal == nil && message.From == ""
}
