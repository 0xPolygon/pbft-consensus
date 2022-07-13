package replay

import (
	"log"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/0xPolygon/pbft-consensus/e2e/helper"
	"github.com/0xPolygon/pbft-consensus/e2e/replay"
)

// backend implements the e2e.IntegrationBackend interface and implements its own pbft.BuildProposal method for replay
type backend struct {
	e2e.Fsm
	messageReader *replay.MessageReader
}

// newBackend is the constructor of Backend
func newBackend(messageReader *replay.MessageReader) *backend {
	return &backend{
		messageReader: messageReader,
	}
}

// BuildProposal builds the next proposal. If it has a preprepare message for given height in .flow file it will take the proposal from file, otherwise it will generate a new one
func (f *backend) BuildProposal() (*pbft.Proposal, error) {
	var data []byte
	sequence := f.Height()
	if prePrepareMessage, exists := f.messageReader.GetPrePrepareMessages(sequence); exists && prePrepareMessage != nil {
		data = prePrepareMessage.Proposal
	} else {
		log.Printf("[WARNING] Could not find PRE-PREPARE message for sequence: %v", sequence)
		data = helper.GenerateProposal()
	}

	return &pbft.Proposal{
		Data: data,
		Time: time.Now().Add(1 * time.Second),
		Hash: helper.Hash(data),
	}, nil
}
