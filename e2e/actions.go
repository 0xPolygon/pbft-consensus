package e2e

import (
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

type RevertFunc func()

type FunctionalAction interface {
	CanApply(c *Cluster) bool
	Apply(c *Cluster) RevertFunc
}

// DropNodeAction encapsulates logic for dropping nodes action.
type DropNodeAction struct {
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

	c.StopNode(nodeToStop.name)

	return func() {
		log.Printf("Reverting stopped node %v\n", nodeToStop.name)
		nodeToStop.Start()
	}
}

type PartitionAction struct {
}

func (action *PartitionAction) CanApply(c *Cluster) bool {
	return true
}

func (action *PartitionAction) Apply(c *Cluster) RevertFunc {
	c.lock.Lock()
	defer c.lock.Unlock()
	hook := newPartitionTransport(500 * time.Millisecond)
	// create 2 partition with random number of nodes
	// minority with less than quorum size nodes and majority with the rest of the nodes
	quorumSize := pbft.QuorumSize(len(c.nodes))

	var minorityPartition []string
	var majorityPartition []string
	minorityPartitionSize := rand.Intn(quorumSize + 1)
	i := 0
	for n := range c.nodes {
		if i < minorityPartitionSize {
			minorityPartition = append(minorityPartition, n)
			i++
		} else {
			majorityPartition = append(majorityPartition, n)
		}
	}
	log.Printf("Partitions ratio %d/%d, [%v], [%v]\n", len(majorityPartition), len(minorityPartition), majorityPartition, minorityPartition)
	hook.Partition(minorityPartition, majorityPartition)

	c.hook = hook

	return func() {
		c.lock.Lock()
		defer c.lock.Unlock()
		log.Println("Reverting partitions.")
		c.hook.Reset()
	}
}
