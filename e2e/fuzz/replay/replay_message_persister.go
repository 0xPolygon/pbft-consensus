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

// replayMessagePersister encapsulates logic for saving messages in .flow file
type replayMessagePersister struct {
	lock     sync.Mutex
	messages []*ReplayMessage
	file     *os.File
}

// saveMetaData saves node meta data to .flow file
func (r *replayMessagePersister) saveMetaData(nodeNames *[]string) error {
	var err error
	if err = r.createFile(); err != nil {
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

// saveCachedMessages saves currently cached messages and timeouts to .flow file
func (r *replayMessagePersister) saveCachedMessages() error {
	r.lock.Lock()
	defer r.lock.Unlock()

	var err error
	if err = r.createFile(); err != nil {
		return err
	}

	if r.messages != nil {
		err = r.saveMessages(r.file)
	}

	return err
}

// createFile creates a .flow file to save messages and timeouts on the predifined location
func (r *replayMessagePersister) createFile() error {
	if r.file == nil {
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
		r.file = file
	}

	return nil
}

// closeFile closes file created by the ReplayMessagesHandler if it is open
func (r *replayMessagePersister) closeFile() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// addMessage adds a message from sequence to messages cache that will be written to .flow file
func (r *replayMessagePersister) addMessage(message *ReplayMessage) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.messages = append(r.messages, message)
}

// saveMessages saves ReplayMessages to the JSON file within the pre-defined directory.
func (r *replayMessagePersister) saveMessages(fileWriter *os.File) error {
	rawMessages, err := ConvertToByteArrays(r.messages)
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

	r.messages = nil
	return nil
}
