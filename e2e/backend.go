package e2e

import (
	"fmt"
	"strconv"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e/helper"
)

// IntegrationBackend extends the pbft.Backend interface with additional e2e-related method
type IntegrationBackend interface {
	pbft.Backend
	SetBackendData(n *node)
}

// BackendFake implements IntegrationBackend interface
type BackendFake struct {
	nodes           []string
	height          uint64
	lastProposer    pbft.NodeID
	proposalAddTime time.Duration

	insertFunc   func(*pbft.SealedProposal) error
	isStuckFunc  func(uint64) (uint64, bool)
	validateFunc func(*pbft.Proposal) error
}

func (bf *BackendFake) BuildProposal() (*pbft.Proposal, error) {
	tm := time.Now()
	if bf.proposalAddTime > 0 {
		tm = tm.Add(bf.proposalAddTime)
	}

	proposal := &pbft.Proposal{
		Data: helper.GenerateProposal(),
		Time: tm,
	}
	proposal.Hash = helper.Hash(proposal.Data)
	return proposal, nil
}

func (bf *BackendFake) Height() uint64 {
	return bf.height
}

func (bf *BackendFake) Init(*pbft.RoundInfo) {
}

func (bf *BackendFake) Insert(p *pbft.SealedProposal) error {
	if bf.insertFunc != nil {
		return bf.insertFunc(p)
	}
	return nil
}

func (bf *BackendFake) IsStuck(num uint64) (uint64, bool) {
	if bf.isStuckFunc != nil {
		return bf.isStuckFunc(num)
	}
	panic("IsStuck " + strconv.Itoa(int(num)))
}

func (bf *BackendFake) Validate(proposal *pbft.Proposal) error {
	if bf.validateFunc != nil {
		return bf.validateFunc(proposal)
	}

	return nil
}

func (bf *BackendFake) ValidatorSet() pbft.ValidatorSet {
	valsAsNode := []pbft.NodeID{}
	for _, i := range bf.nodes {
		valsAsNode = append(valsAsNode, pbft.NodeID(i))
	}

	return &helper.NodeID{
		Nodes:        valsAsNode,
		LastProposer: bf.lastProposer,
	}
}

func (bf *BackendFake) ValidateCommit(from pbft.NodeID, seal []byte) error {
	return nil
}

// SetBackendData implements IntegrationBackend interface and sets the data needed for backend
func (bf *BackendFake) SetBackendData(n *node) {
	bf.nodes = n.nodes
	bf.lastProposer = n.c.getProposer(n.getSyncIndex())
	bf.height = n.GetNodeHeight() + 1
	bf.proposalAddTime = 1 * time.Second
	bf.isStuckFunc = n.isStuck
	bf.insertFunc = n.insert
	bf.validateFunc = func(proposal *pbft.Proposal) error {
		if n.isFaulty() {
			return fmt.Errorf("validation error")
		}

		return nil
	}
}
