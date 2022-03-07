package types

import (
	"encoding/json"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

type ReplayMessage struct {
	To      pbft.NodeID      `json:"to"`
	Timeout time.Duration    `json:"value"`
	Message *pbft.MessageReq `json:"message"`
}

func NewReplayMessageReq(to pbft.NodeID, message *pbft.MessageReq) *ReplayMessage {
	return &ReplayMessage{
		To:      to,
		Message: message,
	}
}

func NewReplayTimeoutMessage(to pbft.NodeID, timeout time.Duration) *ReplayMessage {
	return &ReplayMessage{
		To:      to,
		Timeout: timeout,
	}
}

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
