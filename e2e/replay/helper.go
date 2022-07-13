package replay

import (
	"encoding/json"

	"github.com/0xPolygon/pbft-consensus"
)

// convertToByteArrays converts ReplayMessage slice to JSON representation and return it back as slice of byte arrays
func convertToByteArrays(messages []*message) ([][]byte, error) {
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
