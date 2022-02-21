package e2e

import (
	"log"
	"math/rand"
	"sync"

	"github.com/0xPolygon/pbft-consensus"
)

type RevertFunc func()

type FunctionalAction interface {
	CanApply(c *Cluster) bool
	Apply(c *Cluster) RevertFunc
}

// Encapsulates logic for dropping nodes action.
type DropNodeAction struct {
	lock sync.Mutex
}

func (dn *DropNodeAction) CanApply(c *Cluster) bool {
	runningNodes := len(c.GetRunningNodes())
	if runningNodes <= 0 {
		return false
	}
	maxFaultyNodes := pbft.MaxFaultyNodes(len(c.nodes))
	remainingNodes := runningNodes - 1
	return remainingNodes >= maxFaultyNodes
}

func (dn *DropNodeAction) Apply(c *Cluster) RevertFunc {
	runningNodes := c.GetRunningNodes()
	nodeToStop := runningNodes[rand.Intn(len(runningNodes))]
	log.Printf("Dropping node: '%s'.", nodeToStop)

	dn.lock.Lock()
	c.StopNode(nodeToStop.name)
	dn.lock.Unlock()

	return func() {
		dn.lock.Lock()
		defer dn.lock.Unlock()
		nodeToStop.Start()
	}
}
