package e2e

import (
	"fmt"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/stretchr/testify/assert"
)

func TestE2E_Partition_Liveness(t *testing.T) {
	const nodesCnt = 5
	flow1 := msgFlow{
		round: 0,
		// lock A_0 and A_1 on one proposal
		partition: map[pbft.NodeID][]pbft.NodeID{
			"A_0": {"A_0", "A_1", "A_3"},
			"A_1": {"A_0", "A_1"},
			"A_3": {"A_0", "A_1"},
		},
	}

	flow2 := msgFlow{
		round: 1,
		// lock A_2 and A_3 on one proposal
		partition: map[pbft.NodeID][]pbft.NodeID{
			"A_0": {"A_0", "A_1", "A_2", "A_3", "A_4"},
			"A_1": {"A_0", "A_1", "A_2", "A_3", "A_4"},

			"A_2": {"A_0", "A_1", "A_2", "A_3", "A_4"},
			"A_3": {"A_0", "A_1", "A_2", "A_3"},
			"A_4": {"A_0", "A_1", "A_2", "A_3"}, // from a4
		},
	}

	flowMap := make(map[uint64]msgFlow)
	flowMap[0] = flow1 // for round 0
	flowMap[1] = flow2 // for round 1
	hook := newRoundChange(flowMap)

	c := newPBFTCluster(t, "liveness_issue", "A", nodesCnt, hook)
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(3, 5*time.Minute)

	// log to check what is the end state
	for _, n := range c.nodes {
		fmt.Printf("Node %v, isProposalLocked: %v, proposal data: %v\n", n.name, n.pbft.IsStateLocked(), n.pbft.Proposal().Data)
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
