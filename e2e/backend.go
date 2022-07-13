package e2e

import (
	"strconv"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

type BackendFake struct {
	nodes            []string
	height           uint64
	ProposalTime     time.Duration
	nodeId           int
	IsStuckMock      func(num uint64) (uint64, bool)
	ValidatorSetList pbft.ValidatorSet
	ValidatorSetMock func(fake *BackendFake) pbft.ValidatorSet
	finalProposals   *finalProposal
}

func (bf *BackendFake) BuildProposal() (*pbft.Proposal, error) {
	proposal := &pbft.Proposal{
		Data: GenerateProposal(),
		Time: time.Now(),
	}
	proposal.Hash = Hash(proposal.Data)
	return proposal, nil
}

func (bf *BackendFake) Validate(proposal *pbft.Proposal) error {
	return nil
}

func (bf *BackendFake) Insert(p *pbft.SealedProposal) error {
	if bf.finalProposals != nil {
		return bf.finalProposals.Insert(*p)
	}
	return nil
}

func (bf *BackendFake) Height() uint64 {
	return bf.height
}

func (bf *BackendFake) ValidatorSet() pbft.ValidatorSet {
	if bf.ValidatorSetMock != nil {
		return bf.ValidatorSetMock(bf)
	}
	valsAsNode := []pbft.NodeID{}
	for _, i := range bf.nodes {
		valsAsNode = append(valsAsNode, pbft.NodeID(i))
	}
	vv := pbft.ValStringStub(valsAsNode)
	return &vv
}

func (bf *BackendFake) Init(info *pbft.RoundInfo) {
}

func (bf *BackendFake) IsStuck(num uint64) (uint64, bool) {
	if bf.IsStuckMock != nil {
		return bf.IsStuckMock(num)
	}
	panic("IsStuck " + strconv.Itoa(int(num)))
}

func (bf *BackendFake) ValidateCommit(from pbft.NodeID, seal []byte) error {
	return nil
}

// finalProposal struct contains inserted final proposals for the node
type finalProposal struct {
	lock sync.Mutex
	bc   map[uint64]pbft.Proposal
}

func (i *finalProposal) Insert(proposal pbft.SealedProposal) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	if p, ok := i.bc[proposal.Number]; ok {
		if !p.Equal(proposal.Proposal) {
			panic("wrong proposal inserted")
		}
	} else {
		i.bc[proposal.Number] = *proposal.Proposal
	}
	return nil
}
