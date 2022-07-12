package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const directoryPath = "../SavedState"

// MessagePersister encapsulates logic for saving messages in .flow file
type MessagePersister struct {
	lock     sync.Mutex
	messages []*Message
	file     *os.File
}

// SaveMetaData saves node meta data to .flow file
func (r *MessagePersister) SaveMetaData(nodeNames *[]string) error {
	var err error
	if err = r.CreateFile(); err != nil {
		return err
	}

	bufWriter := bufio.NewWriter(r.file)
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

// SaveCachedMessages saves currently cached messages and timeouts to .flow file
func (r *MessagePersister) SaveCachedMessages() error {
	r.lock.Lock()
	defer r.lock.Unlock()

	var err error
	if err = r.CreateFile(); err != nil {
		return err
	}

	if r.messages != nil {
		err = r.SaveMessages(r.file)
	}

	return err
}

// CreateFile creates a .flow file to save messages and timeouts on the predifined location
func (r *MessagePersister) CreateFile() error {
	if r.file == nil {
		if _, err := os.Stat(directoryPath); os.IsNotExist(err) {
			err := os.Mkdir(directoryPath, 0777)
			if err != nil {
				return err
			}
		}

		path, err := filepath.Abs(directoryPath)
		if err != nil {
			return err
		}

		file, err := os.OpenFile(filepath.Join(path, fmt.Sprintf("%v_%v.flow", FileName, time.Now().Format(time.RFC3339))), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
		if err != nil {
			return err
		}
		r.file = file
	}

	return nil
}

// CloseFile closes file created by the ReplayMessagesHandler if it is open
func (r *MessagePersister) CloseFile() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// AddMessage adds a message from sequence to message cache that will be written to .flow file
func (r *MessagePersister) AddMessage(message *Message) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.messages = append(r.messages, message)
}

// SaveMessages saves ReplayMessages to the JSON file within the pre-defined directory.
func (r *MessagePersister) SaveMessages(fileWriter *os.File) error {
	rawMessages, err := ConvertToByteArrays(r.messages)
	if err != nil {
		return err
	}

	bufWriter := bufio.NewWriterSize(fileWriter, maxCharactersPerLine)
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

	r.messages = nil
	return nil
}
