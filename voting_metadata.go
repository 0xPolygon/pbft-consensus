package pbft

// NewVotingMetadata initializes instance of VotingMetadata
func NewVotingMetadata(votingPower map[NodeID]uint64) *VotingMetadata {
	v := &VotingMetadata{votingPower: votingPower}
	v.maxFaultyVotingPower = v.calculateMaxFaultyVotingPower()
	v.quorumSize = 2*v.MaxFaultyVotingPower() + 1
	return v
}

// VotingMetadata holds weighted vote for each validator. Weighted vote is based on its voting power (e.g. stake amount).
// Voting power is used as input to calculate:
//  - QuorumSize of entire validator set, M
//  - MaxFaultyVotingPower, which is maximum tollerable value of malicious voting power in order to have Byzantine fault tollerant property satisfied.
type VotingMetadata struct {
	votingPower          map[NodeID]uint64
	maxFaultyVotingPower uint64
	quorumSize           uint64
}

// calculateMaxFaultyVotingPower calculates max faulty voting power as at most 1/3 of total voting power of the entire validator set.
func (v *VotingMetadata) calculateMaxFaultyVotingPower() uint64 {
	totalVotingPower := uint64(0)
	for _, v := range v.votingPower {
		totalVotingPower += v
	}
	if totalVotingPower == 0 {
		return 0
	}
	return (totalVotingPower - 1) / 3
}

// QuorumSize calculates quorum size (namely the number of required messages of some type in order to proceed to the next state in PBFT state machine).
// It is calculated by formula:
// 2 * F + 1, where F denotes maximum count of faulty nodes in order to have Byzantine fault tollerant property satisfied.
func (v *VotingMetadata) QuorumSize() uint64 {
	return v.quorumSize
}

// MaxFaultyVotingPower is calculated as at most 1/3 of total voting power of the entire validator set.
func (v *VotingMetadata) MaxFaultyVotingPower() uint64 {
	return v.maxFaultyVotingPower
}

// CalculateVotingPower calculates voting power of provided senders
func (v *VotingMetadata) CalculateVotingPower(senders []NodeID) uint64 {
	accumulatedVotingPower := uint64(0)
	for _, nodeId := range senders {
		accumulatedVotingPower += v.votingPower[nodeId]
	}
	return accumulatedVotingPower
}
