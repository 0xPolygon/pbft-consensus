package e2e

import "github.com/0xPolygon/pbft-consensus"

type ValidatorSet struct {
	Nodes        []pbft.NodeID
	LastProposer pbft.NodeID
	VP           map[pbft.NodeID]uint64
}

func (n *ValidatorSet) CalcProposer(round uint64) pbft.NodeID {
	seed := uint64(0)
	if n.LastProposer == "" {
		seed = round
	} else {
		offset := 0
		if indx := n.Index(n.LastProposer); indx != -1 {
			offset = indx
		}
		seed = uint64(offset) + round + 1
	}

	pick := seed % uint64(n.Len())

	return (n.Nodes)[pick]
}

func (n *ValidatorSet) Index(addr pbft.NodeID) int {
	for indx, i := range n.Nodes {
		if i == addr {
			return indx
		}
	}
	return -1
}

func (n *ValidatorSet) Includes(id pbft.NodeID) bool {
	for _, i := range n.Nodes {
		if i == id {
			return true
		}
	}
	return false
}

func (n *ValidatorSet) Len() int {
	return len(n.Nodes)
}

//voting power stub
func (n *ValidatorSet) VotingPower() map[pbft.NodeID]uint64 {
	return n.VP
}
