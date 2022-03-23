package e2e

import (
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

const flowMapThreshold = 70

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
		nodeToStop.Start()
	}
}

type PartitionAction struct {
}

func (action *PartitionAction) CanApply(_ *Cluster) bool {
	return true
}

func (action *PartitionAction) Apply(c *Cluster) RevertFunc {
	c.lock.Lock()
	defer c.lock.Unlock()
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
		c.lock.Lock()
		defer c.lock.Unlock()
		c.hook.Reset()
	}
}

type FlowMapAction struct {
}

func (f *FlowMapAction) Apply(c *Cluster) RevertFunc {
	// for each node in the cluster add >= (n-1)/3 other connected nodes
	c.lock.Lock()
	defer c.lock.Unlock()
	flowMap := make(map[string][]string)
	for senderNodeId := range c.nodes {
		recipients, ok := flowMap[senderNodeId]
		if !ok {
			recipients = []string{}
			flowMap[senderNodeId] = recipients
		}
		done := false
		// generate map for every node in the cluster
		for recipientNodeId := range c.nodes {
			minValidNodes := c.MinValidNodes()
			if len(recipients) <= minValidNodes && !done {
				recipients = append(recipients, recipientNodeId)
				flowMap[senderNodeId] = recipients
				quorum := 0
				for _, existingRecipientIds := range flowMap {
					if len(existingRecipientIds) >= minValidNodes {
						quorum++
					}
					if quorum >= minValidNodes {
						// we have enough for consensus but with probability add mode connected nodes
						if ShouldApply(flowMapThreshold) {
							continue
						} else {
							done = true
						}
					}
				}
			} else {
				// probabilistic add more nodes to the flow map
				if ShouldApply(flowMapThreshold) {
					recipients = append(recipients, recipientNodeId)
					flowMap[senderNodeId] = recipients
				}
			}
		}
	}

	hook := newFlowMapTransport(flowMap)
	c.hook = hook

	log.Printf("Generated flow map: %v\n", flowMap)

	return func() {
		log.Println("Reverting flow map.")
		c.lock.Lock()
		defer c.lock.Unlock()
		hook.Reset()
	}
}

func (f *FlowMapAction) CanApply(_ *Cluster) bool {
	return true
}
