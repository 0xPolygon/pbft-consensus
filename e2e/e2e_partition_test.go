package e2e

import (
	"testing"
	"time"
)

func TestE2E_Partition_OneMajority(t *testing.T) {
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", 5, hook)
	c.Start()

	c.WaitForHeight(5, 1*time.Minute)

	// create two partitions.
	hook.Partition([]string{"prt_0", "prt_1", "prt_2"}, []string{"prt_3", "prt_4"})

	// only the majority partition will be able to sync
	c.WaitForHeight(10, 1*time.Minute, []string{"prt_0", "prt_1", "prt_2"})

	// the partition with two nodes is stuck
	c.IsStuck(10*time.Second, []string{"prt_3", "prt_4"})

	// TODO: Reset and check that they can sync again
	c.Stop()
}
