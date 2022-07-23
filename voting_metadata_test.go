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
		TotalNodesCount, QuorumSize uint64
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
		metadata := &NonWeightedVotingMetadata{nodesCount: uint(c.TotalNodesCount)}
		assert.Equal(t, c.QuorumSize, metadata.QuorumSize())
	}
}

func TestNonWeightedVotingMetadata_GetRequiredMessagesCount(t *testing.T) {

	cases := []struct {
		TotalNodesCount, RequiredMsgsCount int
	}{
		{1, 0},
		{2, 0},
		{3, 0},
		{4, 2},
		{5, 2},
		{6, 2},
		{7, 4},
		{8, 4},
		{9, 4},
		{10, 6},
		{100, 66},
	}
	for _, c := range cases {
		metadata := &NonWeightedVotingMetadata{nodesCount: uint(c.TotalNodesCount)}
		assert.Equal(t, c.RequiredMsgsCount, metadata.getRequiredMessagesCount())
	}
}
