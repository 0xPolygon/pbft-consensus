package fuzzy

import (
	"crypto/sha1"
	"time"

	"github.com/0xPolygon/ibft-consensus"
)

type fsm struct {
	nodes     []string
	proposals []*ibft.Proposal2
}

func (f *fsm) currentHeight() uint64 {
	number := uint64(1) // initial height is always 1 since 0 is the genesis
	if len(f.proposals) != 0 {
		number = f.proposals[len(f.proposals)-1].Number
	}
	return number
}

func (f *fsm) BuildBlock() (*ibft.Proposal, error) {
	proposal := &ibft.Proposal{
		Data: []byte{byte(f.currentHeight())},
		Time: time.Now().Add(1 * time.Second),
	}
	return proposal, nil
}

func (f *fsm) Validate(proposal []byte) ([]byte, error) {
	// always validate for now
	return nil, nil
}

func (f *fsm) Insert(pp *ibft.Proposal2) error {
	f.proposals = append(f.proposals, pp)
	return nil
}

func (f *fsm) ValidatorSet() (*ibft.Snapshot, error) {
	valsAsNode := []ibft.NodeID{}
	for _, i := range f.nodes {
		valsAsNode = append(valsAsNode, ibft.NodeID(i))
	}
	vv := valString{
		nodes: valsAsNode,
	}
	// set the last proposer if any
	if len(f.proposals) != 0 {
		vv.lastProposer = f.proposals[len(f.proposals)-1].Proposer
	}

	// get the current number from last proposal if any (otherwise 0)
	snap := &ibft.Snapshot{
		ValidatorSet: &vv,
		Number:       f.currentHeight(),
	}
	return snap, nil
}

func (f *fsm) Hash(p []byte) ([]byte, error) {
	h := sha1.New()
	h.Write(p)
	return h.Sum(nil), nil
}

type valString struct {
	nodes        []ibft.NodeID
	lastProposer ibft.NodeID
}

func (v *valString) CalcProposer(round uint64) ibft.NodeID {
	seed := uint64(0)
	if v.lastProposer == ibft.NodeID("") {
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

func (v *valString) Index(addr ibft.NodeID) int {
	for indx, i := range v.nodes {
		if i == addr {
			return indx
		}
	}
	return -1
}

func (v *valString) Includes(id ibft.NodeID) bool {
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
