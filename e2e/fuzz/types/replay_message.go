package types

import (
	"encoding/json"

	"github.com/0xPolygon/pbft-consensus"
)

//Struct that represents a single json object in .flow file
type ReplayMessage struct {
	To      pbft.NodeID      `json:"to"`
	Message *pbft.MessageReq `json:"message"`
}

//Creates a new message to be written to .flow file
func NewReplayMessageReq(to pbft.NodeID, message *pbft.MessageReq) *ReplayMessage {
	return &ReplayMessage{
		To:      to,
		Message: message,
	}
}

//Creates a new timeout to be written to .flow file
func NewReplayTimeoutMessage(to pbft.NodeID) *ReplayMessage {
	return &ReplayMessage{
		To: to,
	}
}

//Indicates if replay message is a timeout
func (rm *ReplayMessage) IsTimeoutMessage() bool {
	return rm.Message == nil
}

// Convert ReplayMessage slice to JSON representation and return it back as slice of byte arrays.
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
