package e2e

import (
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFuzz_Unreliable_Network(t *testing.T) {
	isFuzzEnabled(t)

	rand.Seed(time.Now().Unix())
	nodesCount := 20 + rand.Intn(11) // vary nodes [20,30]
	maxFaulty := nodesCount/3 - 1
	maxHeight := uint64(40)
	currentHeight := uint64(0)
	jitterMax := 500 * time.Millisecond
	hook := newPartitionTransport(jitterMax)
	c := newPBFTCluster(t, "network_unreliable", "prt", nodesCount, hook)
	t.Logf("Starting cluster with %d nodes, max faulty %d.\n", nodesCount, maxFaulty)
	c.Start()
	defer c.Stop()

	for {
		currentHeight += 5
		var minorityPartition []string
		var majorityPartition []string
		// create 2 partition with random number of nodes
		// minority with no more that maxFaulty and majority with rest of the nodes
		pSize := 1 + rand.Intn(maxFaulty)
		for i := 0; i < pSize; i++ {
			minorityPartition = append(minorityPartition, "prt_"+strconv.Itoa(i))
		}
		for i := pSize; i < nodesCount; i++ {
			majorityPartition = append(majorityPartition, "prt_"+strconv.Itoa(i))
		}
		t.Logf("Partitions ratio %d/%d\n", len(majorityPartition), len(minorityPartition))

		hook.Partition(minorityPartition, majorityPartition)
		t.Logf("Checking for height %v, started with nodes %d\n", currentHeight, nodesCount)
		err := c.WaitForHeight(currentHeight, 10*time.Minute, majorityPartition)
		if err != nil {
			t.Fatal(err)
		}

		// randomly drop if possible nodes from the partition pick one number
		dropN := rand.Intn(maxFaulty - pSize + 1)
		t.Logf("Dropping: %v nodes.\n", dropN)

		currentHeight += 5
		// stop N nodes from majority partition
		for i := 0; i < dropN; i++ {
			c.nodes["prt_"+strconv.Itoa(pSize+i)].Stop()
		}

		var runningMajorityNodes []string
		var stoppedNodes []string
		for _, v := range c.nodes {
			if v.IsRunning() {
				for _, bp := range majorityPartition {
					if bp == v.name { // is part of the bigPartition
						runningMajorityNodes = append(runningMajorityNodes, v.name)
					}
				}
			} else {
				stoppedNodes = append(stoppedNodes, v.name)
			}
		}
		// check all running nodes in majority partition for the block height
		t.Logf("Checking for height %v, started with nodes %d\n", currentHeight, nodesCount)
		err = c.WaitForHeight(currentHeight, 10*time.Minute, runningMajorityNodes)
		assert.NoError(t, err)

		// restart network for this iteration
		hook.Reset()
		for _, stopped := range stoppedNodes {
			c.nodes[stopped].Start()
		}

		if currentHeight >= maxHeight {
			break
		}
	}
	hook.Reset()
	// all nodes in the network should be synced after starting all nodes and partition restart
	finalHeight := maxHeight + 10
	t.Logf("Checking final height %v, nodes: %d\n", finalHeight, nodesCount)
	err := c.WaitForHeight(finalHeight, 20*time.Minute)
	assert.NoError(t, err)
}
