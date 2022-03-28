package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_ClusterInsertFinalProposal(t *testing.T) {
	c := newPBFTCluster(t, "cluster", "N", 3)

	// valid proposal => insert it
	seq1Proposal := newSealedProposal([]byte{0x1}, "N0", 1)
	err := c.insertFinalProposal(seq1Proposal)
	assert.Nil(t, err)
	assert.Len(t, c.sealedProposals, 1)

	// invalid proposal (different proposal data on sequence previously inserted) => discard it and return an error
	seq1DiffProposal := newSealedProposal([]byte{0x3}, "N0", 1)
	err = c.insertFinalProposal(seq1DiffProposal)
	assert.NotNil(t, err)
	assert.Len(t, c.sealedProposals, 1)

	// same proposal data on same sequence previously entered => discard it, but don't return an error
	err = c.insertFinalProposal(seq1Proposal)
	assert.Nil(t, err)
	assert.Len(t, c.sealedProposals, 1)

	// sequence-gapped proposal => discard it and return an error
	seq5Proposal := newSealedProposal([]byte{0x5}, "N1", 5)
	err = c.insertFinalProposal(seq5Proposal)
	assert.NotNil(t, err)
	assert.Len(t, c.sealedProposals, 1)

	// valid proposal => insert it
	seq2Proposal := newSealedProposal([]byte{0x2}, "N1", 2)
	c.insertFinalProposal(seq2Proposal)
	assert.Len(t, c.sealedProposals, 2)
}
