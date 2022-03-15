package replay

import (
	"encoding/json"

	"github.com/0xPolygon/pbft-consensus"
)

// ReplayMessage is a struct that represents a single json object in .flow file
type ReplayMessage struct {
	To      pbft.NodeID      `json:"to"`
	Message *pbft.MessageReq `json:"message"`
}

// NewReplayMessageReq creates a new message to be written to .flow file
func NewReplayMessageReq(to pbft.NodeID, message *pbft.MessageReq) *ReplayMessage {
	return &ReplayMessage{
		To:      to,
		Message: message,
	}
}

// NewReplayTimeoutMessage creates a new timeout to be written to .flow file
func NewReplayTimeoutMessage(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) *ReplayMessage {
	return &ReplayMessage{
		To: to,
		Message: &pbft.MessageReq{
			Type: msgType,
			View: view,
		},
	}
}

// ConvertToByteArrays converts ReplayMessage slice to JSON representation and return it back as slice of byte arrays
func ConvertToByteArrays(messages []*ReplayMessage) ([][]byte, error) {
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
