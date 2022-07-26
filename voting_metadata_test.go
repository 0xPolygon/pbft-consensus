package pbft

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestVotingMetadata_MaxFaultyVotingPower_EqualVotingPower(t *testing.T) {
	cases := []struct {
		nodesCount, faultyNodesCount uint
	}{
		{0, 0},
		{1, 0},
		{2, 0},
		{3, 0},
		{4, 1},
		{5, 1},
		{6, 1},
		{7, 2},
		{8, 2},
		{9, 2},
		{10, 3},
		{99, 32},
		{100, 33},
	}
	for _, c := range cases {
		votingPowers := CreateEqualWeightValidatorsMap(createValidatorIds(c.nodesCount))
		metadata := NewVotingMetadata(votingPowers)
		assert.Equal(t, c.faultyNodesCount, uint(metadata.MaxFaultyVotingPower()))
	}
}

func TestVotingMetadata_QuorumSize_EqualVotingPower(t *testing.T) {
	cases := []struct {
		nodesCount uint
		quorumSize uint64
	}{
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 3},
		{5, 3},
		{6, 3},
		{7, 5},
		{8, 5},
		{9, 5},
		{10, 7},
		{100, 67},
	}

	for _, c := range cases {
		votingPowers := CreateEqualWeightValidatorsMap(createValidatorIds(c.nodesCount))
		metadata := NewVotingMetadata(votingPowers)
		assert.Equal(t, c.quorumSize, metadata.QuorumSize())
	}
}

func TestVotingMetadata_CalculateWeight_EqualVotingPower(t *testing.T) {
	cases := []struct {
		nodesCount uint
		weight     uint64
	}{
		{1, 1},
		{3, 3},
		{4, 4},
		{100, 100},
	}

	for _, c := range cases {
		votingPower := CreateEqualWeightValidatorsMap(createValidatorIds(c.nodesCount))
		metadata := NewVotingMetadata(votingPower)
		senders := make([]NodeID, len(votingPower))
		i := 0
		for nodeId := range votingPower {
			senders[i] = nodeId
			i++
		}
		assert.Equal(t, c.weight, metadata.CalculateVotingPower(senders))
	}
}

func TestVotingMetadata_MaxFaultyVotingPower_MixedVotingPower(t *testing.T) {
	cases := []struct {
		votingPower    map[NodeID]uint64
		maxFaultyNodes uint64
	}{
		{map[NodeID]uint64{"A": 0, "B": 0, "C": 0}, 0},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 5},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 6},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 33},
	}
	for _, c := range cases {
		metadata := &WeightedVotingMetadata{votingPower: c.votingPower}
		assert.Equal(t, c.maxFaultyNodes, metadata.MaxFaultyVotingPower())
	}
}

func TestVotingMetadata_QuorumSize_MixedVotingPower(t *testing.T) {
	cases := []struct {
		votingPower map[NodeID]uint64
		quorumSize  uint64
	}{
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 13},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 11},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 67},
	}
	for _, c := range cases {
		metadata := NewVotingMetadata(c.votingPower)
		assert.Equal(t, c.quorumSize, metadata.QuorumSize())
	}
}

func TestVotingMetadata_CalculateWeight_MixedVotingPower(t *testing.T) {
	cases := []struct {
		votingPower map[NodeID]uint64
		quorumSize  uint64
	}{
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 20},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 16},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 100},
	}
	for _, c := range cases {
		metadata := NewVotingMetadata(c.votingPower)
		senders := make([]NodeID, len(c.votingPower))
		i := 0
		for nodeId := range c.votingPower {
			senders[i] = nodeId
			i++
		}
		assert.Equal(t, c.quorumSize, metadata.CalculateVotingPower(senders))
	}
}

func createValidatorIds(validatorsCount uint) []NodeID {
	validatorIds := make([]NodeID, validatorsCount)
	for i := uint(0); i < validatorsCount; i++ {
		validatorIds[i] = NodeID(fmt.Sprintf("NODE_%s", strconv.Itoa(int(i+1))))
	}
	return validatorIds
}
