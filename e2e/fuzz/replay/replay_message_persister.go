package replay

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e"
)

const directoryPath = "../SavedState"

// messagePersister is an interface for storing messages, node and action meta data to .flow files
type messagePersister interface {
	saveMetaData(nodeNames []string, lastSequences []*e2e.MetaData) error
	saveCachedMessages() error
	addAction(action *e2e.MetaData)
	addMessage(message *ReplayMessage)
	closeFile() error
}

// defaultMessagePersister is a default implementation of messagePersister interface
type defaultMessagePersister struct{}

func (d *defaultMessagePersister) saveMetaData(nodeNames []string, lastSequences []*e2e.MetaData) error {
	return nil
}
func (d *defaultMessagePersister) saveCachedMessages() error         { return nil }
func (d *defaultMessagePersister) addAction(action *e2e.MetaData)    {}
func (d *defaultMessagePersister) addMessage(message *ReplayMessage) {}
func (d *defaultMessagePersister) closeFile() error                  { return nil }

// replayMessagePersister encapsulates logic for saving messages in .flow file
type replayMessagePersister struct {
	lock         sync.Mutex
	timestamp    string
	messages     []*ReplayMessage
	messagesFile *os.File
	metaDataFile *os.File
	actions      []*e2e.MetaData
}

// saveMetaData saves node meta data to .flow file
func (r *replayMessagePersister) saveMetaData(nodeNames []string, lastSequences []*e2e.MetaData) error {
	var err error
	var file *os.File
	if r.metaDataFile == nil {
		if file, err = r.createFile(MetaDataFilePrefix); err != nil {
			return err
		}
		r.metaDataFile = file
	}

	bufWriter := bufio.NewWriterSize(r.metaDataFile, maxCharactersPerLine)
	currentRawMessage, err := json.Marshal(nodeNames)
	if err != nil {
		return err
	}

	_, err = bufWriter.Write(currentRawMessage)
	if err != nil {
		return err
	}

	bufWriter.Write([]byte("\n"))

	raw, err := e2e.ConvertActionsToByteArrays(lastSequences)
	if err != nil {
		log.Printf("[ERROR] An error occurred while converting last sequences to byte arrays")
	} else {
		r.writeToFile(raw, bufWriter)
	}

	r.saveActions(bufWriter)

	return err
}

// saveCachedMessages saves currently cached messages and timeouts to .flow file
func (r *replayMessagePersister) saveCachedMessages() error {
	r.lock.Lock()
	defer r.lock.Unlock()

	var err error
	var file *os.File
	if r.messagesFile == nil {
		if file, err = r.createFile(MessagesFilePrefix); err != nil {
			return err
		}
		r.messagesFile = file
	}

	if r.messages != nil {
		err = r.saveMessages()
	}

	return err
}

// addAction adds an action that happened in fuzz to cache that will be saved to .flow file on cluster Stop
func (r *replayMessagePersister) addAction(action *e2e.MetaData) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.actions = append(r.actions, action)
}

// addMessage adds a message from sequence to messages cache that will be written to .flow file
func (r *replayMessagePersister) addMessage(message *ReplayMessage) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.messages = append(r.messages, message)
}

// saveMessages saves ReplayMessages to the JSON file within the pre-defined directory.
func (r *replayMessagePersister) saveMessages() error {
	rawMessages, err := ConvertMessagesToByteArrays(r.messages)
	if err != nil {
		return err
	}

	bufWritter := bufio.NewWriterSize(r.messagesFile, maxCharactersPerLine)
	r.writeToFile(rawMessages, bufWritter)
	r.messages = nil

	return nil
}

// saveActions saves cached actions to .flow file
func (r *replayMessagePersister) saveActions(bufWritter *bufio.Writer) {
	rawActions, err := e2e.ConvertActionsToByteArrays(r.actions)
	if err != nil {
		log.Printf("[ERROR] An error occurred while converting actions to byte arrays")
		return
	}

	if len(rawActions) == 0 { //no actions were done in fuzz
		return
	}

	r.writeToFile(rawActions, bufWritter)
}

// writeToFile writes that to designated file using bufio writer
func (r *replayMessagePersister) writeToFile(data [][]byte, bufWriter *bufio.Writer) error {

	for _, rawMessage := range data {
		_, err := bufWriter.Write(rawMessage)
		if err != nil {
			return err
		}

		_, err = bufWriter.Write([]byte("\n"))
		if err != nil {
			return err
		}
	}

	bufWriter.Flush()
	return nil
}

// createFile creates a .flow file to save messages and timeouts on the predifined location
func (r *replayMessagePersister) createFile(filePrefix string) (*os.File, error) {
	if r.timestamp == "" {
		r.timestamp = time.Now().Format(time.RFC3339)
	}

	directory := fmt.Sprintf("%v_%v", directoryPath, r.timestamp)
	if _, err := os.Stat(directory); os.IsNotExist(err) {
		err := os.Mkdir(directory, 0777)
		if err != nil {
			return nil, err
		}
	}

	path, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}

	file, err := os.OpenFile(filepath.Join(path, fmt.Sprintf("%v.flow", filePrefix)), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
	if err != nil {
		return nil, err
	}

	return file, err
}

// closeFile closes file created by the ReplayMessagesHandler if it is open
func (r *replayMessagePersister) closeFile() error {
	var errStrings []string
	if r.messagesFile != nil {
		if err := r.messagesFile.Close(); err != nil {
			errStrings = append(errStrings, err.Error())
		}
	}

	if r.metaDataFile != nil {
		if err := r.metaDataFile.Close(); err != nil {
			errStrings = append(errStrings, err.Error())
		}
	}

	if len(errStrings) > 0 {
		return fmt.Errorf(strings.Join(errStrings, "\n"))
	}

	return nil
}
