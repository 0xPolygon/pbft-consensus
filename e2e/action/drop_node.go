package action

import (
	"log"
	"math/rand"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

// DropNode encapsulates logic for dropping nodes action.
type DropNode struct {
}

func (dn *DropNode) CanApply(c *e2e.Cluster) bool {
	runningNodes := len(c.GetRunningNodes())
	if runningNodes <= 0 {
		return false
	}
	maxFaultyNodes := pbft.MaxFaultyNodes(len(c.Nodes()))
	remainingNodes := runningNodes - 1
	return remainingNodes >= maxFaultyNodes
}

func (dn *DropNode) Apply(c *e2e.Cluster) RevertFunc {
	runningNodes := c.GetRunningNodes()
	nodeToStop := runningNodes[rand.Intn(len(runningNodes))]
	log.Printf("Dropping node: '%s'.", nodeToStop)

	c.StopNode(nodeToStop.GetName())

	return func() {
		log.Printf("Reverting stopped node %v\n", nodeToStop.GetName())
		nodeToStop.Start()
	}
}
