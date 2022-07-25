package pbft

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNonWeightedVotingMetadata_MaxFaulty(t *testing.T) {
	cases := []struct {
		TotalNodesCount, FaultyNodesCount uint
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
		metadata := &NonWeightedVotingMetadata{nodesCount: c.TotalNodesCount}
		assert.Equal(t, c.FaultyNodesCount, uint(metadata.MaxFaultyWeight()))
	}
}

func TestNonWeightedVotingMetadata_QuorumSize(t *testing.T) {
	cases := []struct {
		TotalNodesCount uint
		QuorumSize      uint64
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
		metadata := &NonWeightedVotingMetadata{nodesCount: c.TotalNodesCount}
		assert.Equal(t, c.QuorumSize, metadata.QuorumSize())
	}
}

func TestWeightedVotingMetadata_MaxFaultyWeight(t *testing.T) {
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
		metadata := &WeightedVotingMetadata{votingPowerMap: c.votingPower}
		assert.Equal(t, c.maxFaultyNodes, metadata.MaxFaultyWeight())
	}
}

func TestWeightedVotingMetadata_QuorumSize(t *testing.T) {
	cases := []struct {
		votingPower map[NodeID]uint64
		quorumSize  uint64
	}{
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 11},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 13},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 67},
	}
	for _, c := range cases {
		metadata := &WeightedVotingMetadata{votingPowerMap: c.votingPower}
		assert.Equal(t, c.quorumSize, metadata.QuorumSize())
	}
}

func TestWeightedVotingMetadata_CalculateWeight(t *testing.T) {
	cases := []struct {
		votingPower map[NodeID]uint64
		quorumSize  uint64
	}{
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 16},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 20},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 100},
	}
	for _, c := range cases {
		metadata := &WeightedVotingMetadata{votingPowerMap: c.votingPower}
		messages := make(map[NodeID]*MessageReq, len(c.votingPower))
		for nodeId := range c.votingPower {
			messages[nodeId] = &MessageReq{}
		}
		assert.Equal(t, c.quorumSize, metadata.CalculateWeight(messages))
	}
}
