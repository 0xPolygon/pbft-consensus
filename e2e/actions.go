package e2e

import (
	"fmt"
	"log"
	"math/rand"

	"github.com/0xPolygon/pbft-consensus"
)

type Action interface {
	Apply(c *Cluster)
	Revert(c *Cluster)
}

// Encapsulates logic for dropping nodes action.
type DropNodeAction struct {
}

func (dn *DropNodeAction) Validate(c *Cluster) error {
	runningNodes := len(c.GetRunningNodes())
	if runningNodes <= 0 {
		return fmt.Errorf("no nodes are currently running")
	}
	maxFaultyNodes := pbft.MaxFaultyNodes(len(c.nodes))
	remainingNodes := runningNodes - 1
	if remainingNodes < maxFaultyNodes {
		return fmt.Errorf("dropping a node would jeopardize Byzantine fault-tollerant conditions.\nExpected at least %d nodes to run, but action would leave it to %d",
			maxFaultyNodes, remainingNodes)
	}
	return nil

}

func (dn *DropNodeAction) Apply(c *Cluster) {
	if err := dn.Validate(c); err != nil {
		log.Printf("[WARNING] Skipping drop node action. Reason: '%s'", err)
		return
	}

	maxFaultyNodes := pbft.MaxFaultyNodes(len(c.nodes))
	runningNodes := c.GetRunningNodes()
	log.Printf("BEGIN Running nodes: %v", runningNodes)

	// Stop a node with some probability
	nodeToStop := runningNodes[rand.Intn(len(runningNodes))]
	log.Printf("Dropping node '%s'.", nodeToStop)
	c.StopNode(nodeToStop.name)

	// Keep number of running nodes above max faulty nodes count
	stoppedNodes := c.GetStoppedNodes()
	if len(stoppedNodes) > maxFaultyNodes {
		// Randomly choose a node, from the set of stopped nodes to be started again
		nodeToStart := stoppedNodes[rand.Intn(len(stoppedNodes))]
		log.Printf("Starting node '%s'.", nodeToStart)
		nodeToStart.Start()
	}
	log.Printf("END Running nodes: %v", c.GetRunningNodes())
}

// Revert reverts all nodes that are not running
func (dn *DropNodeAction) Revert(c *Cluster) {
	log.Println("Reverting dropped nodes")
	for _, node := range c.GetStoppedNodes() {
		node.Start()
	}
}
