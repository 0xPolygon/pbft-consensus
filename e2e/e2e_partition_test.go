package e2e

import (
	"testing"
	"time"
)

func TestE2E_Partition_OneMajority(t *testing.T) {
	const nodesCnt = 5
	const nodesPerPartition = 3
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	c.Start()

	c.WaitForHeight(5, 1*time.Minute, false)

	// create two partitions.
	partitions := getPartitions(nodesCnt, nodesPerPartition, "prt_")
	hook.Partition(partitions...)

	// only the majority partition will be able to sync
	c.WaitForHeight(10, 1*time.Minute, false, partitions[0])

	// the partition with two nodes is stuck
	c.IsStuck(10*time.Second, partitions[1])

	// reset all partitions
	hook.Reset()

	allNodes := make([]string, len(c.nodes))
	for i, node := range c.Nodes() {
		allNodes[i] = node.name
	}
	// all nodes should be able to sync
	c.WaitForHeight(15, 1*time.Minute, false, allNodes)

	c.Stop()
}
