package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

const (
	FileName             = "messages"
	MaxCharactersPerLine = 2048 * 1024 // Increase Scanner buffer size to 2MB per line
)

// ReplayMessagesNotifier is a struct that implements ReplayNotifier interface
type ReplayMessagesNotifier struct {
	lock              sync.Mutex
	messages          []*ReplayMessage
	file              *os.File
	msgProcessingDone chan struct{}
}

// NewReplayMessagesNotifier creates a new instance of ReplayMessageNotifier
func NewReplayMessagesNotifier(channelBuffer int) *ReplayMessagesNotifier {
	return &ReplayMessagesNotifier{
		msgProcessingDone: make(chan struct{}, channelBuffer),
	}
}

// SaveMetaData saves node meta data to .flow file
func (h *ReplayMessagesNotifier) SaveMetaData(nodeNames *[]string) error {
	var err error
	if err = h.createFile(); err != nil {
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

// SaveState saves currently cached messages and timeouts to .flow file
func (h *ReplayMessagesNotifier) SaveState() error {
	h.lock.Lock()
	defer h.lock.Unlock()

	var err error
	if err = h.createFile(); err != nil {
		return err
	}

	if h.messages != nil {
		err = h.saveMessages(h.file)
	}

	return err
}

// HandleMessage caches processed message to be saved later in .flow file
func (h *ReplayMessagesNotifier) HandleMessage(to pbft.NodeID, message *pbft.MessageReq) {
	h.addMessage(NewReplayMessageReq(to, message))
}

// HandleTimeout is an implementation of StateNotifier interface
func (h *ReplayMessagesNotifier) HandleTimeout(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) {
	h.addMessage(NewReplayTimeoutMessage(to, msgType, view))
}

// ReadNextMessage is an implementation of StateNotifier interface
func (h *ReplayMessagesNotifier) ReadNextMessage(p *pbft.Pbft) (*pbft.MessageReq, []*pbft.MessageReq) {
	msg, discards := p.ReadMessageWithDiscards()

	if msg == nil {
		if !p.HasMessages() && h.msgProcessingDone != nil {
			//when the next message is null, and queues are empty, we know we drained the message queue of the given node
			h.msgProcessingDone <- struct{}{}
		}
	} else if isTimeoutMessage(msg) {
		return nil, nil
	}

	return msg, discards
}

// createFile creates a .flow file to save messages and timeouts on the predifined location
func (h *ReplayMessagesNotifier) createFile() error {
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

		file, err := os.OpenFile(filepath.Join(path, fmt.Sprintf("%v_%v.flow", FileName, time.Now().Format(time.RFC3339))), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
		if err != nil {
			return err
		}
		h.file = file
	}

	return nil
}

// CloseFile closes file created by the ReplayMessagesHandler if it is open
func (h *ReplayMessagesNotifier) CloseFile() error {
	if h.file != nil {
		return h.file.Close()
	}
	return nil
}

// addMessage adds a message from sequence to messages cache that will be written to .flow file
func (h *ReplayMessagesNotifier) addMessage(message *ReplayMessage) {
	h.lock.Lock()
	defer h.lock.Unlock()
	h.messages = append(h.messages, message)
}

// saveMessages saves ReplayMessages to the JSON file within the pre-defined directory.
func (h *ReplayMessagesNotifier) saveMessages(fileWriter *os.File) error {
	rawMessages, err := ConvertToByteArrays(h.messages)
	if err != nil {
		return err
	}

	bufWriter := bufio.NewWriterSize(fileWriter, MaxCharactersPerLine)
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

func isTimeoutMessage(message *pbft.MessageReq) bool {
	return message.Hash == nil && message.Proposal == nil && message.Seal == nil && message.From == ""
}
