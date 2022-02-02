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
// Correctness Analysis of Istanbul Byzantine Fault Tolerance(https://arxiv.org/pdf/1901.07160.pdf).
// Specific problem this test is emulating is described in Chapter 7.1, Case 1.
// Summary of the problem is that there are not enough nodes to lock on single proposal,
// due to some issues where nodes being unable to deliver messages to all of the peers.
// Therefore nodes are split into two subsets locked on different proposals.
// Simulating one node from larger subset as faulty one, results in being unable to reach a consensus on a single proposal and that's what liveness issue is about.
func TestE2E_Partition_Liveness(t *testing.T) {
	const nodesCnt = 5
	round0 := roundMetadata{
		round: 0,
		// lock A_3 and A_4 on one proposal
		routingMap: map[pbft.NodeID][]pbft.NodeID{
			"A_0": {"A_3", "A_4"},
			"A_3": {"A_0", "A_3", "A_4"},
			"A_4": {"A_3", "A_4"},
		},
	}

	round1 := roundMetadata{
		round: 1,
		// lock A_2 and A_0 on one proposal and A_1 will be faulty
		routingMap: map[pbft.NodeID][]pbft.NodeID{
			"A_0": {"A_0", "A_2", "A_3", "A_4"},
			"A_1": {"A_0", "A_2", "A_3", "A_4"},
			"A_2": {"A_0", "A_1", "A_2", "A_3", "A_4"},

			"A_3": {"A_0", "A_1", "A_2", "A_3", "A_4"},
			"A_4": {"A_0", "A_1", "A_2", "A_3", "A_4"},
		},
	}
	flowMap := map[uint64]roundMetadata{0: round0, 1: round1}

	livenessGossipArbitrage := func(sender, receiver pbft.NodeID, msg *pbft.MessageReq) bool {
		faultyNodeId := pbft.NodeID("A_1")
		if msg.View.Sequence > 2 || msg.View.Round > 1 {
			// node A_1 (faulty) is unresponsive after round 1
			if sender == faultyNodeId || receiver == faultyNodeId {
				return false
			}
			// all other nodes are connected for all the messages
			return true
		}

		// Case where we are in round 1 and 2 different nodes will lock the proposal
		// (A_1 ignores round change and commit messages)
		if msg.View.Round == 1 {
			if sender == faultyNodeId &&
				(msg.Type == pbft.MessageReq_RoundChange || msg.Type == pbft.MessageReq_Commit) {
				return false
			}
		}

		msgFlow, ok := flowMap[msg.View.Round]
		if !ok {
			return false
		}

		if msgFlow.round == msg.View.Round {
			receivers, ok := msgFlow.routingMap[sender]
			if !ok {
				return false
			}

			// do not send commit messages for rounds <=1
			if msg.Type == pbft.MessageReq_Commit {
				return false
			}

			foundReceiver := false
			for _, v := range receivers {
				if v == receiver {
					foundReceiver = true
					break
				}
			}
			return foundReceiver
		}
		return true
	}

	hook := newGenericGossipTransport(livenessGossipArbitrage)

	c := newPBFTCluster(t, "liveness_issue", "A", nodesCnt, hook)
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(3, 5*time.Minute)

	// log to check what is the end state
	for _, n := range c.nodes {
		t.Logf("Node %v, isProposalLocked: %v, proposal data: %v\n", n.name, n.pbft.IsStateLocked(), n.pbft.Proposal().Data)
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
