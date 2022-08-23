package e2e

import "github.com/0xPolygon/pbft-consensus"

type ValidatorSet struct {
	Nodes        []pbft.NodeID
	LastProposer pbft.NodeID
}

func (v *ValidatorSet) CalcProposer(round uint64) pbft.NodeID {
	seed := uint64(0)
	if v.LastProposer == "" {
		seed = round
	} else {
		offset := 0
		if indx := v.Index(v.LastProposer); indx != -1 {
			offset = indx
		}
		seed = uint64(offset) + round + 1
	}

	pick := seed % uint64(v.Len())

	return (v.Nodes)[pick]
}

func (v *ValidatorSet) Index(addr pbft.NodeID) int {
	for indx, i := range v.Nodes {
		if i == addr {
			return indx
		}
	}
	return -1
}

func (v *ValidatorSet) Includes(id pbft.NodeID) bool {
	for _, i := range v.Nodes {
		if i == id {
			return true
		}
	}
	return false
}

func (v *ValidatorSet) Len() int {
	return len(v.Nodes)
}

func (v *ValidatorSet) VerifySeal(nodeID pbft.NodeID, seal, proposalHash []byte) error {
	return nil
}
