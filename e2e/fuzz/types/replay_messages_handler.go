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

type ReplayMessagesHandler struct {
	lock     sync.Mutex
	messages []*ReplayMessage
	file     *os.File
}

func (h *ReplayMessagesHandler) CloseFile() {
	if h.file != nil {
		h.file.Close()
	}
}

func (h *ReplayMessagesHandler) AddMessage(message *ReplayMessage) {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.messages = append(h.messages, message)
}

func (h *ReplayMessagesHandler) SaveState() error {
	h.lock.Lock()
	defer h.lock.Unlock()

	var err error
	if h.file == nil {
		relativePath := "../SavedState"
		if _, err := os.Stat(relativePath); os.IsNotExist(err) {
			err := os.Mkdir(relativePath, 0777)
			if err != nil {
				return err
			}
		}

		path, err := filepath.Abs(relativePath)
		if err != nil {
			return err
		}

		file, err := os.OpenFile(filepath.Join(path, FileName), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
		if err != nil {
			return err
		}
		h.file = file
	}

	if h.messages != nil {
		err = h.Save(h.file)
	}

	return err
}

// Save ReplayMessages to the JSON file within the pre-defined directory.
func (h *ReplayMessagesHandler) Save(fileWritter *os.File) error {
	rawMessages, err := ConvertToByteArrays(h.messages)
	if err != nil {
		return err
	}

	bufWriter := bufio.NewWriter(fileWritter)
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

// Load ReplayMessages JSON representation from the file on filePath and deserialize it into the object model.
func Load(filePath string) ([]*ReplayMessage, error) {
	messages := make([]*ReplayMessage, 0)
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
		var message *ReplayMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			return messages, err
		}
		messages = append(messages, message)
	}

	return messages, nil
}

func (h *ReplayMessagesHandler) HandleMessage(to pbft.NodeID, message *pbft.MessageReq) {
	h.AddMessage(NewReplayMessageReq(to, message))
}

func (h *ReplayMessagesHandler) HandleTimeout(to pbft.NodeID, timeout time.Duration) {
	h.AddMessage(NewReplayTimeoutMessage(to, timeout))
}
