package e2e

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e/helper"
)

// CreateBackend is a delegate that creates a new instance of IntegrationBackend interface
type CreateBackend func() IntegrationBackend

// IntegrationBackend extends the pbft Backend interface with additional methods
type IntegrationBackend interface {
	pbft.Backend
	SetBackendData(n *node)
}

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
		Data: helper.GenerateProposal(),
		Time: time.Now(),
	}
	proposal.Hash = helper.Hash(proposal.Data)
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

// -- fsm --

func newSealedProposal(proposalData []byte, proposer pbft.NodeID, number uint64) *pbft.SealedProposal {
	proposal := &pbft.Proposal{
		Data: proposalData,
		Time: time.Now(),
	}
	proposal.Hash = helper.Hash(proposal.Data)
	return &pbft.SealedProposal{
		Proposal: proposal,
		Proposer: proposer,
		Number:   number,
	}
}

type Fsm struct {
	n               *node
	nodes           []string
	lastProposer    pbft.NodeID
	height          uint64
	validationFails bool
}

func (f *Fsm) Height() uint64 {
	return f.height
}

func (f *Fsm) IsStuck(num uint64) (uint64, bool) {
	return f.n.isStuck(num)
}

func (f *Fsm) BuildProposal() (*pbft.Proposal, error) {
	proposal := &pbft.Proposal{
		Data: helper.GenerateProposal(),
		Time: time.Now().Add(1 * time.Second),
	}
	proposal.Hash = helper.Hash(proposal.Data)
	return proposal, nil
}

func (f *Fsm) Validate(proposal *pbft.Proposal) error {
	if f.validationFails {
		return fmt.Errorf("validation error")
	}
	return nil
}

func (f *Fsm) Insert(pp *pbft.SealedProposal) error {
	return f.n.Insert(pp)
}

func (f *Fsm) ValidatorSet() pbft.ValidatorSet {
	valsAsNode := []pbft.NodeID{}
	for _, i := range f.nodes {
		valsAsNode = append(valsAsNode, pbft.NodeID(i))
	}
	vv := valString{
		nodes:        valsAsNode,
		lastProposer: f.lastProposer,
	}
	return &vv
}

// SetBackendData implements IntegrationBackend interface and sets the data needed for backend
func (f *Fsm) SetBackendData(n *node) {
	f.n = n
	f.nodes = n.nodes
	f.lastProposer = n.c.getProposer(n.getSyncIndex())
	f.height = n.GetNodeHeight() + 1
	f.validationFails = n.isFaulty()
}

func (f *Fsm) Init(*pbft.RoundInfo) {
}

func (f *Fsm) ValidateCommit(node pbft.NodeID, seal []byte) error {
	return nil
}

type valString struct {
	nodes        []pbft.NodeID
	lastProposer pbft.NodeID
}

func (v *valString) CalcProposer(round uint64) pbft.NodeID {
	seed := uint64(0)
	if v.lastProposer == pbft.NodeID("") {
		seed = round
	} else {
		offset := 0
		if indx := v.Index(v.lastProposer); indx != -1 {
			offset = indx
		}
		seed = uint64(offset) + round + 1
	}

	pick := seed % uint64(v.Len())

	return (v.nodes)[pick]
}

func (v *valString) Index(addr pbft.NodeID) int {
	for indx, i := range v.nodes {
		if i == addr {
			return indx
		}
	}
	return -1
}

func (v *valString) Includes(id pbft.NodeID) bool {
	for _, i := range v.nodes {
		if i == id {
			return true
		}
	}
	return false
}

func (v *valString) Len() int {
	return len(v.nodes)
}
