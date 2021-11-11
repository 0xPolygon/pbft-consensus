package e2e

import (
	"testing"
	"time"
)

func TestE2E_Partition_OneMajority(t *testing.T) {
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newIBFTCluster(t, "prt", 5, hook)
	c.Start()

	c.WaitForHeight(5, 1*time.Minute)

	// create two partitions.
	hook.Partition([]string{"prt_0", "prt_1", "prt_2"}, []string{"prt_3", "prt_4"})

	// only the majority partition will be able to sync
	c.WaitForHeight(10, 1*time.Minute, []string{"prt_0", "prt_1", "prt_2"})

	// the partition with two nodes is stuck
	c.IsStuck(10*time.Second, []string{"prt_3", "prt_4"})

	// add a new partition that connects prt_4 and prt_0. Prt_4 should join the ensemble
	hook.Partition([]string{"prt_4", "prt_0"})

	// TODO: We cannot expect a specific height, we need to check for some good iterations
	// - which block are we in now???

	c.Stop()
}

func TestE2E_Partition_NoMajority(t *testing.T) {

}
