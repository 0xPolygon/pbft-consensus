package types

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

const FileName = "messages_"

//Struct containg data needed to write messages and timeouts to .flow file
type ReplayMessagesHandler struct {
	lock     sync.Mutex
	messages []*ReplayMessage
	file     *os.File
}

//Adds message from sequence to messages cache that will be written to .flow file
func (h *ReplayMessagesHandler) AddMessage(message *ReplayMessage) {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.messages = append(h.messages, message)
}

//Saves node meta data to .flow file
func (h *ReplayMessagesHandler) SaveMetaData(nodeNames *[]string) error {
	var err error
	if err = h.CreateFile(); err != nil {
		return err
	}

	bufWriter := bufio.NewWriter(h.file)
	defer bufWriter.Flush()

	currentRawMessage, err := json.Marshal(nodeNames)
	if err != nil {
		return err
	}

	_, err = bufWriter.Write(currentRawMessage)
	if err != nil {
		return err
	}

	_, err = bufWriter.Write([]byte("\n"))

	return err
}

//Saves currently cached messages and timeouts to .flow file
func (h *ReplayMessagesHandler) SaveState() error {
	h.lock.Lock()
	defer h.lock.Unlock()

	var err error
	if err = h.CreateFile(); err != nil {
		return err
	}

	if h.messages != nil {
		err = h.SaveMessages(h.file)
	}

	return err
}

// Saves ReplayMessages to the JSON file within the pre-defined directory.
func (h *ReplayMessagesHandler) SaveMessages(fileWritter *os.File) error {
	rawMessages, err := ConvertToByteArrays(h.messages)
	if err != nil {
		return err
	}

	bufWriter := bufio.NewWriter(fileWritter)
	defer bufWriter.Flush()

	for _, rawMessage := range rawMessages {
		_, err = bufWriter.Write(rawMessage)
		if err != nil {
			return err
		}

		_, err = bufWriter.Write([]byte("\n"))
		if err != nil {
			return err
		}
	}

	h.messages = nil
	return nil
}

// Loads ReplayMessages JSON representation from the file on filePath and deserialize it into the object model.
func Load(filePath string) ([]*ReplayMessage, []string, error) {
	messages := make([]*ReplayMessage, 0)
	nodeNames := make([]string, 0)

	_, err := os.Stat(filePath)
	if err != nil {
		return messages, nodeNames, err
	}

	flowsFile, err := os.Open(filePath)
	if err != nil {
		return messages, nodeNames, err
	}
	defer flowsFile.Close()

	scanner := bufio.NewScanner(flowsFile)

	const MaxCharactersPerLine = 2048 * 1024 // Increase Scanner buffer size to 2MB per line
	buffer := []byte{}
	scanner.Buffer(buffer, MaxCharactersPerLine)

	scanner.Scan() // first line carries the node names needed to create appropriate number of nodes for replay
	if err := json.Unmarshal(scanner.Bytes(), &nodeNames); err != nil {
		return messages, nodeNames, err
	}

	for scanner.Scan() {
		var message *ReplayMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			return messages, nodeNames, err
		}
		messages = append(messages, message)
	}

	return messages, nodeNames, nil
}

func (h *ReplayMessagesHandler) HandleMessage(to pbft.NodeID, message *pbft.MessageReq) {
	h.AddMessage(NewReplayMessageReq(to, message))
}

func (h *ReplayMessagesHandler) HandleTimeout(to pbft.NodeID) {
	h.AddMessage(NewReplayTimeoutMessage(to))
}

//Creates a .flow file to save messages and timeouts on the predifined location
func (h *ReplayMessagesHandler) CreateFile() error {
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

		file, err := os.OpenFile(filepath.Join(path, FileName+strconv.FormatInt(time.Now().Unix(), 10)+".flow"), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
		if err != nil {
			return err
		}
		h.file = file
	}

	return nil
}

//Closes file if it is open
func (h *ReplayMessagesHandler) CloseFile() {
	if h.file != nil {
		h.file.Close()
	}
}
