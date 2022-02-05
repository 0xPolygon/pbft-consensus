package e2e

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/stretchr/testify/assert"
)

// Test proves existence of liveness issues which is described in
// Correctness Analysis of Istanbul Byzantine Fault Tolerance (https://arxiv.org/pdf/1901.07160.pdf).
// Specific problem this test is emulating is described in Chapter 7.1, Case 1.
// Summary of the problem is that there are not enough nodes to lock on single proposal,
// due to some issues where nodes being unable to deliver messages to all of the peers.
// Therefore nodes are split into two subsets locked on different proposals.
// Simulating one node from larger subset as faulty one, results in being unable to reach a consensus on a single proposal and that's what liveness issue is about.
// This test creates a single cluster of 5 nodes, but instead of letting all the peers communicate with each other,
// it routes messages only to specific nodes and induces that nodes lock on different proposal.
// In the round=1, it marks one node as faulty (meaning that it doesn't takes part in gossiping).
func TestE2E_Partition_Liveness(t *testing.T) {
	const nodesCnt = 5
	round0 := roundMetadata{
		round: 0,
		// induce locking A_3 and A_4 on one proposal
		routingMap: map[sender]receivers{
			"A_0": {"A_3", "A_4"},
			"A_3": {"A_0", "A_3", "A_4"},
			"A_4": {"A_3", "A_4"},
		},
	}
	round1 := roundMetadata{
		round: 1,
		// induce locking lock A_0 and A_2 on another proposal
		routingMap: map[sender]receivers{
			"A_0": {"A_0", "A_2", "A_3", "A_4"},
			"A_1": {"A_0", "A_2", "A_3", "A_4"},
			"A_2": {"A_0", "A_1", "A_2", "A_3", "A_4"},

			"A_3": {"A_0", "A_1", "A_2", "A_3", "A_4"},
			"A_4": {"A_0", "A_1", "A_2", "A_3", "A_4"},
		},
	}
	flowMap := map[uint64]roundMetadata{0: round0, 1: round1}

	faultyNodeId := pbft.NodeID("A_1")

	transport := newGenericGossipTransport()
	// If livenessGossipHandler returns false, message should not be transported.
	livenessGossipHandler := func(senderId, receiverId pbft.NodeID, msg *pbft.MessageReq) bool {
		if msg.View.Round <= 1 && msg.Type == pbft.MessageReq_Commit {
			// Cut all the commit messages gossiping for round 0 and 1
			return false
		}

		if msg.View.Round > 1 || msg.View.Sequence > 2 {
			// Faulty node is unresponsive after round 1, and all the other nodes are gossiping all the messages.
			return senderId != faultyNodeId && receiverId != faultyNodeId
		} else {
			if msg.View.Round == 1 && senderId == faultyNodeId &&
				(msg.Type == pbft.MessageReq_RoundChange || msg.Type == pbft.MessageReq_Commit) {
				// Case where we are in round 1 and 2 different nodes will lock the proposal
				// (consequence of faulty node doesn't gossip round change and commit messages).
				return false
			}
		}

		return transport.shouldGossipBasedOnMsgFlowMap(msg, senderId, receiverId)
	}

	transport.withFlowMap(flowMap).withGossipHandler(livenessGossipHandler)

	c := newPBFTCluster(t, "liveness_issue", "A", nodesCnt, transport)
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(3, 5*time.Minute)

	// log to check what is the end state
	for _, n := range c.nodes {
		proposal := n.pbft.GetProposal()
		if proposal != nil {
			t.Logf("Node %v, isProposalLocked: %v, proposal data: %v\n", n.name, n.pbft.IsStateLocked(), proposal.Data)
		} else {
			t.Logf("Node %v, isProposalLocked: %v, no proposal set\n", n.name, n.pbft.IsStateLocked())
		}
	}

	assert.NoError(t, err)
}

func TestE2E_Partition_OneMajority(t *testing.T) {
	const nodesCnt = 5
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(5, 1*time.Minute)
	assert.NoError(t, err)

	// create two partitions.
	majorityPartition := []string{"prt_0", "prt_1", "prt_2"}
	minorityPartition := []string{"prt_3", "prt_4"}
	hook.Partition(majorityPartition, minorityPartition)

	// only the majority partition will be able to sync
	err = c.WaitForHeight(10, 1*time.Minute, majorityPartition)
	assert.NoError(t, err)

	// the partition with two nodes is stuck
	c.IsStuck(10*time.Second, minorityPartition)

	// reset all partitions
	hook.Reset()

	allNodes := make([]string, len(c.nodes))
	for i, node := range c.Nodes() {
		allNodes[i] = node.name
	}
	// all nodes should be able to sync
	err = c.WaitForHeight(15, 1*time.Minute, allNodes)
	assert.NoError(t, err)
}

func TestE2E_Partition_MajorityCanValidate(t *testing.T) {
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	limit := int(math.Floor(nodesCnt*2.0/3.0)) + 1 // 2F+1 nodes can Validate
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name >= "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	names := generateNodeNames(0, limit, "prt_")
	err := c.WaitForHeight(4, 1*time.Minute, names)
	assert.NoError(t, err)
	// restart minority and wait to sync
	for _, node := range c.Nodes() {
		if node.name >= "prt_"+strconv.Itoa(limit) {
			node.Restart()
		}
	}
	err = c.WaitForHeight(4, 1*time.Minute)
	assert.NoError(t, err)
}

func TestE2E_Partition_MajorityCantValidate(t *testing.T) {
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name < "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	names := generateNodeNames(limit, nodesCnt, "prt_")
	err := c.WaitForHeight(3, 1*time.Minute, names)
	assert.Errorf(t, err, "Height reached for minority of nodes")
}

func TestE2E_Partition_BigMajorityCantValidate(t *testing.T) {
	const nodesCnt = 100
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name <= "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	nodeNames := generateNodeNames(limit, nodesCnt, "prt_")
	err := c.WaitForHeight(8, 1*time.Minute, nodeNames)
	assert.Errorf(t, err, "Height reached for minority of nodes")
}
