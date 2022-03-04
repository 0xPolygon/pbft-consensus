package types

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

const FileName = "messages.flow"

type RoundMessageHandler struct {
	lock     sync.Mutex
	messages []*RoundMessage
}

func (h *RoundMessageHandler) AddMessage(message *RoundMessage) {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.messages = append(h.messages, message)
}

// Save RoundMessages to the JSON file within the pre-defined directory.
func (h *RoundMessageHandler) Save(directory string) error {
	_, err := os.Stat(directory)
	if err != nil {
		return err
	}

	rawMessages, err := ConvertToByteArrays(h.messages)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(filepath.Join(directory, FileName), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
	if err != nil {
		return err
	}
	defer file.Close()

	bufWriter := bufio.NewWriter(file)
	defer bufWriter.Flush()

	for i, rawMessage := range rawMessages {
		_, err = bufWriter.Write(rawMessage)
		if err != nil {
			return err
		}

		if i != len(rawMessages)-1 {
			_, err = bufWriter.Write([]byte("\r\n"))
			if err != nil {
				return err
			}
		}
	}

	h.messages = nil
	return nil
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

func (h *RoundMessageHandler) HandleMessage(to pbft.NodeID, message *pbft.MessageReq) {
	h.AddMessage(NewRoundMessage(to, message))
}

func (h *RoundMessageHandler) HandleTimeout(to pbft.NodeID, timeout time.Duration) {
	h.AddMessage(NewTimeoutMessage(to, timeout))
}
