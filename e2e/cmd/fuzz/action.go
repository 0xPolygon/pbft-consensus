package fuzz

import (
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/0xPolygon/pbft-consensus/e2e/transport"
)

type RevertFunc func()

// Action represents the action behavior
type Action interface {
	CanApply(c *e2e.Cluster) bool
	Apply(c *e2e.Cluster) RevertFunc
}

// DropNode encapsulates logic for dropping nodes action.
type DropNode struct{}

func (dn *DropNode) CanApply(c *e2e.Cluster) bool {
	runningNodes := len(c.GetRunningNodes())
	if runningNodes <= 0 {
		return false
	}
	maxFaultyNodes, err := c.MaxFaulty()
	if err != nil {
		log.Printf("[ERROR] %v", err)
		return false
	}
	remainingNodes := runningNodes - 1
	return remainingNodes >= int(maxFaultyNodes)
}

func (dn *DropNode) Apply(c *e2e.Cluster) RevertFunc {
	runningNodes := c.GetRunningNodes()
	nodeToStop := runningNodes[rand.Intn(len(runningNodes))] //nolint:golint,gosec
	log.Printf("Dropping node: '%s'.", nodeToStop)

	c.StopNode(nodeToStop.GetName())

	return func() {
		log.Printf("Reverting stopped node %v\n", nodeToStop.GetName())
		nodeToStop.Start()
	}
}

type Partition struct{}

func (action *Partition) CanApply(*e2e.Cluster) bool {
	return true
}

func (action *Partition) Apply(c *e2e.Cluster) RevertFunc {
	nodes := c.Nodes()

	hook := transport.NewPartition(500 * time.Millisecond)
	// create 2 partition with random number of nodes
	// minority with less than quorum size nodes and majority with the rest of the nodes
	quorumSize, err := c.QuorumSize()
	if err != nil {
		log.Printf("[ERROR] %v", err)
		return nil
	}
	var minorityPartition []string
	var majorityPartition []string
	minorityPartitionSize := rand.Intn(int(quorumSize + 1)) //nolint:golint,gosec
	i := 0
	for _, n := range nodes {
		if i < minorityPartitionSize {
			minorityPartition = append(minorityPartition, n.GetName())
			i++
		} else {
			majorityPartition = append(majorityPartition, n.GetName())
		}
	}
	log.Printf("Partitions ratio %d/%d, [%v], [%v]\n", len(majorityPartition), len(minorityPartition), majorityPartition, minorityPartition)
	hook.Partition(minorityPartition, majorityPartition)

	c.SetHook(hook)

	return func() {
		log.Println("Reverting partitions.")
		if tHook := c.GetTransportHook(); tHook != nil {
			tHook.Reset()
		}
	}
}

func getAvailableActions() []Action {
	return []Action{
		&DropNode{},
		&Partition{},
	}
}
