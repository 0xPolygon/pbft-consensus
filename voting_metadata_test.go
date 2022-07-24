package pbft

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNonWeightedVotingMetadata_MaxFaultyNodesCount(t *testing.T) {
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
		assert.Equal(t, c.FaultyNodesCount, uint(metadata.MaxFaultyNodes()))
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

func TestWeightedVotingMetadata_MaxFaultyNodes(t *testing.T) {

	cases := []struct {
		votingPower    map[NodeID]uint64
		maxFaultyNodes uint64
	}{
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 6},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 33},
	}
	for _, c := range cases {
		metadata := &WeightedVotingMetadata{votingPowerMap: c.votingPower}
		assert.Equal(t, c.maxFaultyNodes, metadata.MaxFaultyNodes())
	}
}
