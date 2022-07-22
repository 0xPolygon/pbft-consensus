package pbft

type VotingMetadata interface {
	// QuorumSize returns PBFT message count needed to perform a single PBFT state transition
	QuorumSize() uint64
	// MaxFaultyNodes returns maximum number of faulty nodes,
	// in order to meet practical Byzantine conditions
	MaxFaultyNodes() uint64
}

// NewVotingMetadata initializes instance of VotingMetadata based on provided configuration
func NewVotingMetadata(config *Config, validatorsCount uint) VotingMetadata {
	if IsVotingPowerEnabled(config) {
		return &WeightedVotingMetadata{votingPowerMap: config.VotingPower}
	}
	return &NonWeightedVotingMetadata{nodesCount: validatorsCount}
}

// NonWeightedVotingMetadata implements VotingMetadata interface,
// where each validator has same weight during vote process
// (namely its vote counts the same as the vote of other peers).
type NonWeightedVotingMetadata struct {
	nodesCount uint
}

// QuorumSize calculates quorum size (namely the number of required messages of some type in order to proceed to the next state in PolyBFT state machine).
// It is calculated by formula:
// 2 * F + 1, where F denotes maximum count of faulty nodes in order to have Byzantine fault tollerant property satisfied.
func (n *NonWeightedVotingMetadata) QuorumSize() uint64 {
	return 2*n.MaxFaultyNodes() + 1
}

// MaxFaultyNodes calculate max faulty nodes in order to have Byzantine-fault tollerant system.
// Formula explanation:
// N -> number of nodes in PBFT
// F -> number of faulty nodes
// N = 3 * F + 1 => F = (N - 1) / 3
//
// PBFT tolerates 1 failure with 4 nodes
// 4 = 3 * 1 + 1
// To tolerate 2 failures, PBFT requires 7 nodes
// 7 = 3 * 2 + 1
// It should always take the floor of the result
func (n *NonWeightedVotingMetadata) MaxFaultyNodes() uint64 {
	if n.nodesCount == 0 {
		return 0
	}
	return uint64((n.nodesCount - 1) / 3)
}

// getRequiredMessagesCount returns the number of required messages based on the quorum size
func (n *NonWeightedVotingMetadata) getRequiredMessagesCount() int {
	// 2 * F + 1
	// + 1 is up to the caller to add
	// the current node tallying the messages will include its own message
	return int(n.QuorumSize() - 1)
}

// WeightedVotingMetadata implements VotingMetadata interface,
// where each validator has weighted vote based on its voting power (e.g. stake amount)
type WeightedVotingMetadata struct {
	votingPowerMap map[NodeID]uint64
}

// QuorumSize calculates quorum size (namely the number of required messages of some type in order to proceed to the next state in PolyBFT state machine).
// It is calculated by formula:
// 2 * F + 1, where F denotes maximum count of faulty nodes in order to have Byzantine fault tollerant property satisfied.
func (v *WeightedVotingMetadata) QuorumSize() uint64 {
	return 2*v.MaxFaultyNodes() + 1
}

// MaxFaultyNodes is calculated as at most 1/3 of total voting power of the entire validator set.
func (v *WeightedVotingMetadata) MaxFaultyNodes() uint64 {
	totalVotingPower := v.calculateTotalVotingPower()
	if totalVotingPower == 0 {
		return 0
	}
	return (totalVotingPower - 1) / 3
}

// calculateTotalVotingPower calculates sum of voting powers of single validator node.
func (v *WeightedVotingMetadata) calculateTotalVotingPower() uint64 {
	var totalVotingPower uint64
	for _, v := range v.votingPowerMap {
		totalVotingPower += v
	}
	return totalVotingPower
}

// calculateMessagesVotingPower calculates voting power of validators which are registered in the provided messages map.
func (v *WeightedVotingMetadata) calculateMessagesVotingPower(messages map[NodeID]*MessageReq) uint64 {
	var roundVotingPower uint64
	for nodeId := range messages {
		roundVotingPower += v.votingPowerMap[nodeId]
	}
	return roundVotingPower
}
