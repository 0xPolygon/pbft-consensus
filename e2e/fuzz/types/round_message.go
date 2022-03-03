package types

import (
	"bufio"
	"encoding/json"
	"os"
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

// Load RoundMessages JSON representation from the file on filePath and deserialize it into the object model.
func Load(filePath string) ([]*RoundMessage, error) {
	messages := make([]*RoundMessage, 0)
	_, err := os.Stat(filePath)
	if err != nil {
		return messages, err
	}

	flowsFile, err := os.Open(filePath)
	if err != nil {
		return messages, err
	}
	defer flowsFile.Close()

	scanner := bufio.NewScanner(flowsFile)

	const MaxCharactersPerLine = 2048 * 1024 // Increase Scanner buffer size to 2MB per line
	buffer := []byte{}
	scanner.Buffer(buffer, MaxCharactersPerLine)

	for scanner.Scan() {
		var message *RoundMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			return messages, err
		}
		messages = append(messages, message)
	}

	return messages, nil
}
