package pbft

type VotingMetadata interface {
	// QuorumSize returns PBFT message count needed to perform a single PBFT state transition
	QuorumSize() uint64
	// MaxFaultyVotingPower returns maximum quantity of faulty voting power, in order to meet practical Byzantine conditions
	MaxFaultyVotingPower() uint64
	// CalculateVotingPower returns sum of weights for given messages senders
	CalculateVotingPower(senders []NodeID) uint64
}

// NewVotingMetadata initializes instance of VotingMetadata
func NewVotingMetadata(votingPower map[NodeID]uint64) VotingMetadata {
	return &WeightedVotingMetadata{votingPower: votingPower}
}

// WeightedVotingMetadata implements VotingMetadata interface,
// where each validator has weighted vote based on its voting power (e.g. stake amount)
type WeightedVotingMetadata struct {
	votingPower map[NodeID]uint64
}

// QuorumSize calculates quorum size (namely the number of required messages of some type in order to proceed to the next state in PBFT state machine).
// It is calculated by formula:
// 2 * F + 1, where F denotes maximum count of faulty nodes in order to have Byzantine fault tollerant property satisfied.
func (v *WeightedVotingMetadata) QuorumSize() uint64 {
	return 2*v.MaxFaultyVotingPower() + 1
}

// MaxFaulty is calculated as at most 1/3 of total voting power of the entire validator set.
func (v *WeightedVotingMetadata) MaxFaultyVotingPower() uint64 {
	totalVotingPower := uint64(0)
	for _, v := range v.votingPower {
		totalVotingPower += v
	}
	if totalVotingPower == 0 {
		return 0
	}
	return (totalVotingPower - 1) / 3
}

// CalculateVotingPower calculates voting power of provided senders
func (v *WeightedVotingMetadata) CalculateVotingPower(senders []NodeID) uint64 {
	accumulatedVotingPower := uint64(0)
	for _, nodeId := range senders {
		accumulatedVotingPower += v.votingPower[nodeId]
	}
	return accumulatedVotingPower
}
