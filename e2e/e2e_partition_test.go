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

	// TODO: Reset and check that they can sync again
	c.Stop()
}
