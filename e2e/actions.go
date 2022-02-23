package e2e

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
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

type PartitionAction struct {
}

func (action *PartitionAction) CanApply(c *Cluster) bool {
	return true
}

func (action *PartitionAction) Apply(c *Cluster) RevertFunc {
	hook := newPartitionTransport(500 * time.Millisecond)
	// create 2 partition with random number of nodes
	// minority with no more than max faulty nodes and majority with the rest of the nodes
	maxFaultyNodes := pbft.MaxFaultyNodes(len(c.nodes))

	var minorityPartition []string
	var majorityPartition []string
	minorityPartitionSize := rand.Intn(maxFaultyNodes + 1)
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
		log.Println("Reverting partitions.")
		c.hook.Reset()
	}
}

type FlowMapAction struct {
	// todo can be used map[string][]string or
	//  some better structure like https://pkg.go.dev/github.com/yourbasic/graph

}

// every node must be connected with n = 3f+1 => (n-1)/3

func (f *FlowMapAction) Apply(c *Cluster) RevertFunc {
	// for each node in the cluster add >= (n-1)/3 other connected nodes
	// TODO this can be inside transport layer instead of the action
	flowMap := make(map[string][]string)
	for _, n := range c.nodes {
		if _, ok := flowMap[n.GetName()]; !ok {
			flowMap[n.GetName()] = []string{}
		}
		finish := false
		for _, j := range c.nodes {
			if len(flowMap[n.GetName()]) <= pbft.QuorumSize(len(c.nodes)) && finish == false { // todo check conditions and restructure
				flowMap[n.GetName()] = append(flowMap[n.GetName()], j.GetName())
				quorum := 0
				for i, _ := range flowMap {
					if len(flowMap[i]) >= pbft.QuorumSize(len(c.nodes)) {
						quorum++
					}
					if quorum >= pbft.QuorumSize(len(c.nodes)) {
						// we have enough nodes connected
						if ShouldApply(80) {
							continue
						} else {
							finish = true
						}
					}
				}

			} else {
				// probabilistic add more nodes to the flow map
				if ShouldApply(80) {
					flowMap[n.GetName()] = append(flowMap[n.GetName()], j.GetName())
				}
			}
		}
	}
	hook := newFlowMapTransport()
	hook.flow(flowMap)

	fmt.Printf("Flow map: %v\n", flowMap)

	return func() {
		log.Println("Reverting flow map.")
		hook.Reset()
	}
}

func (f *FlowMapAction) CanApply(c *Cluster) bool {
	return true
}
