package e2e

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

func TestFuzz_NetworkChurn(t *testing.T) {
	rand.Seed(time.Now().Unix())
	nodeCount := 20
	maxFaulty := nodeCount/3 - 1
	const prefix = "ptr_"
	c := newPBFTCluster(t, "network_churn", "ptr", nodeCount)
	c.Start()
	runningNodeCount := nodeCount
	// randomly stop nodes every 3 seconds
	executeInTimerAndWait(3*time.Second, 30*time.Second, func(_ time.Duration) {
		nodeNo := rand.Intn(nodeCount)
		nodeID := prefix + strconv.Itoa(nodeNo)
		node := c.nodes[nodeID]
		if node.IsRunning() && runningNodeCount > nodeCount-maxFaulty {
			// node is running
			c.StopNode(nodeID)
			runningNodeCount--
		} else if !node.IsRunning() {
			// node is not running
			c.StartNode(nodeID)
			runningNodeCount++
		}
	})

	// get all running nodes after random drops
	var runningNodes []string
	for _, v := range c.nodes {
		if v.IsRunning() {
			runningNodes = append(runningNodes, v.name)
		}
	}
	fmt.Println("Checking height after churn")
	// all running nodes must have the same height
	err := c.WaitForHeight(35, 5*time.Minute, runningNodes)
	if err != nil {
		t.Fatal(err)
	}

	// start rest of the nodes
	for _, v := range c.nodes {
		if !v.IsRunning() {
			v.Start()
			runningNodes = append(runningNodes, v.name)
		}
	}
	// all nodes must sync and have same height
	fmt.Println("Checking height after all nodes start")
	err = c.WaitForHeight(45, 5*time.Minute, runningNodes)
	if err != nil {
		t.Fatal(err)
	}
	c.Stop()
}
