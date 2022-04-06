package replay

import (
	"encoding/json"

	"github.com/0xPolygon/pbft-consensus"
)

// MetaData is a struct that holds data about fuzz actions that happened and need to be saved in .flow file
type MetaData struct {
	DataType string `json:"actionType"`
	Data     string `json:"data"`
	Sequence uint64 `json:"sequence"`
	Round    uint64 `json:"round"`
}

// NewMetaData creates a new MetaData object
func NewMetaData(dataType, data string, sequence, round uint64) *MetaData {
	return &MetaData{
		DataType: dataType,
		Data:     data,
		Sequence: sequence,
		Round:    round,
	}
}

// ReplayMessage is a struct that represents a single json object in .flow file
type ReplayMessage struct {
	To      pbft.NodeID      `json:"to"`
	Message *pbft.MessageReq `json:"message"`
}

// NewReplayMessageReq creates a new message to be written to .flow file
func NewReplayMessageReq(to pbft.NodeID, message *pbft.MessageReq) *ReplayMessage {
	return &ReplayMessage{
		To:      to,
		Message: message,
	}
}

// NewReplayTimeoutMessage creates a new timeout to be written to .flow file
func NewReplayTimeoutMessage(to pbft.NodeID, msgType pbft.MsgType, view *pbft.View) *ReplayMessage {
	return &ReplayMessage{
		To: to,
		Message: &pbft.MessageReq{
			Type: msgType,
			View: view,
		},
	}
}

// ConvertToByteArrays is a generic method that converts provided slice to JSON representation and returns it back as slice of byte arrays
func ConvertToByteArrays(items []interface{}) ([][]byte, error) {
	var allRawMessages [][]byte
	for _, message := range items {
		currentRawMessage, err := json.Marshal(message)
		if err != nil {
			return allRawMessages, err
		}
		allRawMessages = append(allRawMessages, currentRawMessage)
	}
	return allRawMessages, nil
}
