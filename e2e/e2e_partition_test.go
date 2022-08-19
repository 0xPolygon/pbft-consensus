package e2e

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e/helper"
	"github.com/0xPolygon/pbft-consensus/e2e/transport"
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
func TestE2E_Partition_LivenessIssue_Case1_FiveNodes_OneFaulty(t *testing.T) {
	t.Parallel()

	round0 := transport.RoundMetadata{
		Round: 0,
		// induce locking A_3 and A_4 on one proposal
		RoutingMap: map[transport.Sender]transport.Receivers{
			"A_0": {"A_3", "A_4"},
			"A_3": {"A_0", "A_3", "A_4"},
			"A_4": {"A_3", "A_4"},
		},
	}
	round1 := transport.RoundMetadata{
		Round: 1,
		// induce locking lock A_0 and A_2 on another proposal
		RoutingMap: map[transport.Sender]transport.Receivers{
			"A_0": {"A_0", "A_2", "A_3", "A_4"},
			"A_1": {"A_0", "A_2", "A_3", "A_4"},
			"A_2": {"A_0", "A_1", "A_2", "A_3", "A_4"},

			"A_3": {"A_0", "A_1", "A_2", "A_3", "A_4"},
			"A_4": {"A_0", "A_1", "A_2", "A_3", "A_4"},
		},
	}
	flowMap := map[uint64]transport.RoundMetadata{0: round0, 1: round1}

	faultyNodeId := pbft.NodeID("A_1")

	transport := transport.NewGenericGossip()
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

		return transport.ShouldGossipBasedOnMsgFlowMap(msg, senderId, receiverId)
	}

	transport.WithFlowMap(flowMap).WithGossipHandler(livenessGossipHandler)

	config := &ClusterConfig{
		Count:  5,
		Name:   "liveness_issue",
		Prefix: "A",
	}

	c := NewPBFTCluster(t, config, transport)
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(3, 5*time.Minute, []string{"A_0", "A_2", "A_3", "A_4"})

	if err != nil {
		// log to check what is the end state
		for _, n := range c.nodes {
			proposal := n.pbft.GetProposal()
			if proposal != nil {
				t.Logf("Node %v, isProposalLocked: %v, proposal data: %v\n", n.name, n.pbft.IsLocked(), proposal.Data)
			} else {
				t.Logf("Node %v, isProposalLocked: %v, no proposal set\n", n.name, n.pbft.IsLocked())
			}
		}
	}

	assert.NoError(t, err)
}

// Test proves existence of liveness issues which is described in
// Correctness Analysis of Istanbul Byzantine Fault Tolerance(https://arxiv.org/pdf/1901.07160.pdf).
// Specific problem this test is emulating is described in Chapter 7.1, Case 2.
// Summary of the problem is that there are not enough nodes to lock on single proposal,
// due to some issues where nodes being unable to deliver messages to all of the peers.
// When there are (nh) = 6 nodes, and they are split into three subsets, all locked on different proposals, where one of the nodes becomes faulty,
// nodes are unable to reach a consensus on a single proposal, resulting in continuous RoundChange.
// This test creates a single cluster of 6 nodes, but instead of letting all the peers communicate with each other,
// it routes messages only to specific nodes and induces that nodes lock on different proposal.
// In the round=3, it marks one node as faulty (meaning that it doesn't takes part in gossiping).
func TestE2E_Partition_LivenessIssue_Case2_SixNodes_OneFaulty(t *testing.T) {
	t.Parallel()

	round0 := transport.RoundMetadata{
		Round: 0,
		// lock A_1, A_4
		RoutingMap: map[transport.Sender]transport.Receivers{
			"A_0": {"A_1", "A_3", "A_4"},
			"A_3": {"A_1", "A_3", "A_4"},
			"A_4": {"A_1", "A_4"},
		},
	}

	round2 := transport.RoundMetadata{
		Round: 2,
		// lock A_5
		RoutingMap: map[transport.Sender]transport.Receivers{
			"A_0": {"A_5", "A_2", "A_4"},
			"A_1": {"A_5", "A_0"},
			"A_2": {"A_5", "A_3"},
			"A_3": {"A_5"},
			"A_4": {"A_5"},
		},
	}

	round3 := transport.RoundMetadata{
		Round: 3,
		// lock A_3 and A_0 on one proposal and A_2 will be faulty
		RoutingMap: map[transport.Sender]transport.Receivers{
			"A_3": {"A_0", "A_2", "A_3", "A_4"},
			"A_0": {"A_0", "A_3", "A_4"},
			"A_2": {"A_0", "A_1", "A_3", "A_4"},
		},
	}
	flowMap := map[uint64]transport.RoundMetadata{0: round0, 2: round2, 3: round3}
	transport := transport.NewGenericGossip()
	faultyNodeId := pbft.NodeID("A_2")

	// If livenessGossipHandler returns false, message should not be transported.
	livenessGossipHandler := func(senderId, receiverId pbft.NodeID, msg *pbft.MessageReq) (sent bool) {
		if msg.View.Round == 1 && msg.Type == pbft.MessageReq_RoundChange {
			return true
		}

		if msg.View.Round > 3 || msg.View.Sequence > 2 {
			// Faulty node is unresponsive after round 3, and all the other nodes are gossiping all the messages.
			return senderId != faultyNodeId && receiverId != faultyNodeId
		} else {
			if msg.View.Round <= 1 && msg.Type == pbft.MessageReq_Commit {
				// Cut all the commit messages gossiping for round 0 and 1
				return false
			}
			if msg.View.Round == 3 && senderId == faultyNodeId &&
				(msg.Type == pbft.MessageReq_RoundChange || msg.Type == pbft.MessageReq_Commit) {
				// Case where we are in round 3 and 2 different nodes will lock the proposal
				// (consequence of faulty node doesn't gossip round change and commit messages).
				return false
			}
		}

		return transport.ShouldGossipBasedOnMsgFlowMap(msg, senderId, receiverId)
	}

	transport.WithFlowMap(flowMap).WithGossipHandler(livenessGossipHandler)

	config := &ClusterConfig{
		Count:  6,
		Name:   "liveness_issue",
		Prefix: "A",
	}

	c := NewPBFTCluster(t, config, transport)
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(3, 5*time.Minute, []string{"A_0", "A_1", "A_3", "A_4", "A_5"})

	if err != nil {
		// log to check what is the end state
		for _, n := range c.nodes {
			proposal := n.pbft.GetProposal()
			if proposal != nil {
				t.Logf("Node %v, isProposalLocked: %v, proposal data: %v\n", n.name, n.pbft.IsLocked(), proposal.Data)
			} else {
				t.Logf("Node %v, isProposalLocked: %v, no proposal set\n", n.name, n.pbft.IsLocked())
			}
		}
	}

	assert.NoError(t, err)
}

// TestE2E_Network_Stuck_Locked_Node_Dropped is a test that creates a situation with no consensus
// one node gets dropped from a network once the proposal gets locked (A_3)
// two nodes are locked on the same proposal (A_0 and A_1)
// and one node is not locked (A_2).
func TestE2E_Network_Stuck_Locked_Node_Dropped(t *testing.T) {
	t.Parallel()

	round0 := transport.RoundMetadata{
		Round: 0,
		RoutingMap: map[transport.Sender]transport.Receivers{
			"A_0": {"A_0", "A_1", "A_3", "A_2"},
			"A_1": {"A_0", "A_1", "A_3"},
			"A_2": {"A_0", "A_1", "A_2", "A_3"},
		},
	}
	flowMap := map[uint64]transport.RoundMetadata{0: round0}
	transport := transport.NewGenericGossip()

	config := &ClusterConfig{
		Count:        4,
		Name:         "liveness_issue",
		Prefix:       "A",
		RoundTimeout: helper.GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config, transport)
	node3 := c.nodes["A_3"]
	// If livenessGossipHandler returns false, message should not be transported.
	gossipHandler := func(senderId, receiverId pbft.NodeID, msg *pbft.MessageReq) (sent bool) {

		// all nodes are connected if sequence is > 1 or round > 0 for sequence 1
		if msg.View.Sequence > 1 || (msg.View.Sequence == 1 && msg.View.Round > 0) {
			return true
		}
		// stop node A_3 once it is locked
		if node3.IsRunning() && node3.IsLocked() {
			node3.Stop()
		}
		return transport.ShouldGossipBasedOnMsgFlowMap(msg, senderId, receiverId)
	}

	transport.WithFlowMap(flowMap).WithGossipHandler(gossipHandler)

	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(3, 1*time.Minute, []string{"A_0", "A_1", "A_2"})

	// log to check what is the end state
	for _, n := range c.nodes {
		proposal := n.pbft.GetProposal()
		if proposal != nil {
			t.Logf("Node %v, running: %v, isProposalLocked: %v, proposal data: %v\n", n.name, n.IsRunning(), n.pbft.IsLocked(), proposal)
		} else {
			t.Logf("Node %v, running: %v, isProposalLocked: %v, no proposal set\n", n.name, n.IsRunning(), n.pbft.IsLocked())
		}
	}
	assert.NoError(t, err)
}

func TestE2E_Partition_OneMajority(t *testing.T) {
	t.Parallel()
	const nodesCnt = 5
	hook := transport.NewPartition(50 * time.Millisecond)

	config := &ClusterConfig{
		Count:        nodesCnt,
		Name:         "majority_partition",
		Prefix:       "prt",
		RoundTimeout: helper.GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config, hook)
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
	c.isStuck(10*time.Second, minorityPartition)

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
	t.Parallel()
	const nodesCnt = 7 // N = 3 * F + 1, F = 2
	hook := transport.NewPartition(50 * time.Millisecond)

	config := &ClusterConfig{
		Count:        nodesCnt,
		Name:         "majority_partition",
		Prefix:       "prt",
		RoundTimeout: helper.GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config, hook)
	limit := int(math.Floor(nodesCnt*2.0/3.0)) + 1 // 2F+1 nodes can Validate
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name >= "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	names := helper.GenerateNodeNames(0, limit, "prt_")
	err := c.WaitForHeight(4, 1*time.Minute, names)
	assert.NoError(t, err)

	// restart minority and wait to sync
	for _, nd := range c.Nodes() {
		if nd.name >= "prt_"+strconv.Itoa(limit) {
			nd.restart()
		}
	}

	err = c.WaitForHeight(4, 1*time.Minute)
	assert.NoError(t, err)
}

func TestE2E_Partition_MajorityCantValidate(t *testing.T) {
	t.Parallel()
	const nodesCnt = 7 // N = 3 * F + 1, F = 2
	hook := transport.NewPartition(50 * time.Millisecond)

	config := &ClusterConfig{
		Count:        nodesCnt,
		Name:         "majority_partition",
		Prefix:       "prt",
		RoundTimeout: helper.GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name < "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	names := helper.GenerateNodeNames(limit, nodesCnt, "prt_")
	err := c.WaitForHeight(3, 1*time.Minute, names)
	assert.Errorf(t, err, "Height reached for minority of nodes")
}

func TestE2E_Partition_BigMajorityCantValidate(t *testing.T) {
	t.Parallel()
	const nodesCnt = 100
	hook := transport.NewPartition(50 * time.Millisecond)

	config := &ClusterConfig{
		Count:        nodesCnt,
		Name:         "majority_partition",
		Prefix:       "prt",
		RoundTimeout: helper.GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name <= "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	nodeNames := helper.GenerateNodeNames(limit, nodesCnt, "prt_")
	err := c.WaitForHeight(8, 1*time.Minute, nodeNames)
	assert.Errorf(t, err, "Height reached for minority of nodes")
}
