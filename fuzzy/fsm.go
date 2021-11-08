package fuzzy

import (
	"crypto/sha1"
	"time"

	"github.com/0xPolygon/ibft-consensus"
)

type fsm struct {
	nodes  []string
	number uint64
}

func (f *fsm) BuildBlock() (*ibft.Proposal, error) {
	proposal := &ibft.Proposal{
		Data: []byte{byte(f.number)},
		Time: time.Now().Add(1 * time.Second),
	}
	return proposal, nil
}

func (f *fsm) Validate(proposal []byte) ([]byte, error) {
	// always validate for now
	return nil, nil
}

func (f *fsm) Insert(proposal []byte, committedSeals [][]byte) error {
	f.number++
	return nil
}

func (f *fsm) ValidatorSet() (*ibft.Snapshot, error) {
	valsAsNode := []ibft.NodeID{}
	for _, i := range f.nodes {
		valsAsNode = append(valsAsNode, ibft.NodeID(i))
	}
	vv := valString(valsAsNode)

	snap := &ibft.Snapshot{
		ValidatorSet: &vv,
		Number:       f.number,
	}
	return snap, nil
}

func (f *fsm) Hash(p []byte) ([]byte, error) {
	h := sha1.New()
	h.Write(p)
	return h.Sum(nil), nil
}

type valString []ibft.NodeID

func (v *valString) CalcProposer(round uint64) ibft.NodeID {
	seed := uint64(0)

	offset := 0
	// add last proposer

	seed = uint64(offset) + round
	pick := seed % uint64(v.Len())

	return (*v)[pick]
}

func (v *valString) Index(addr ibft.NodeID) int {
	for indx, i := range *v {
		if i == addr {
			return indx
		}
	}

	return -1
}

func (v *valString) Includes(id ibft.NodeID) bool {
	for _, i := range *v {
		if i == id {
			return true
		}
	}
	return false
}

func (v *valString) Len() int {
	return len(*v)
}
