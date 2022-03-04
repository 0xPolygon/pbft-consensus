package types

import (
	"encoding/json"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

type RoundMessage struct {
	To      pbft.NodeID      `json:"to"`
	Timeout time.Duration    `json:"value"`
	Message *pbft.MessageReq `json:"message"`
}

func NewRoundMessage(to pbft.NodeID, message *pbft.MessageReq) *RoundMessage {
	return &RoundMessage{
		To:      to,
		Message: message,
	}
}

func NewTimeoutMessage(to pbft.NodeID, timeout time.Duration) *RoundMessage {
	return &RoundMessage{
		To:      to,
		Timeout: timeout,
	}
}

func (rm *RoundMessage) IsTimeoutMessage() bool {
	return rm.Message == nil
}

// Convert RoundMessage slice to JSON representation and return it back as slice of byte arrays.
func ConvertToByteArrays(messages []*RoundMessage) ([][]byte, error) {
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
