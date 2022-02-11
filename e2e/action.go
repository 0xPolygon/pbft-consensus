package e2e

import (
	"fmt"
	"log"
	"math/rand"
	"time"
)

type Action interface {
	Apply(c *Cluster)
	Revert(c *Cluster)
}

// Encapsulates logic for dropping nodes action.
type DropNodeAction struct {
	dropCount                int
	dropProbabilityThreshold int
	// TODO: Idea: Extract these two fields to another action (called PeriodicAction), which would receive as a parameter another action and would run it?
	droppingInterval time.Duration
	duration         time.Duration
}

func newDropNodeAction(dropCount, dropProbabilityThreshold int, droppingInterval, duration time.Duration) *DropNodeAction {
	return &DropNodeAction{
		dropCount:                dropCount,
		dropProbabilityThreshold: dropProbabilityThreshold,
		droppingInterval:         droppingInterval,
		duration:                 duration,
	}
}

func (dn *DropNodeAction) Validate(c *Cluster) error {
	nodesCount := len(c.nodes)
	if dn.dropCount >= nodesCount {
		return fmt.Errorf("trying to drop more nodes (%d) than available in the cluster (%d)", dn.dropCount, nodesCount)
	}
	runningNodes := c.GetRunningNodes()
	nodesLeft := len(runningNodes) - dn.dropCount
	maxFaultyNodes := GetMaxFaultyNodes(nodesCount)
	if nodesLeft < maxFaultyNodes {
		return fmt.Errorf("dropping %d nodes would jeopardize Byzantine fault-tollerant conditions.\nExpected at least %d nodes to run, but action would leave it to %d",
			dn.dropCount, maxFaultyNodes, nodesLeft)
	}
	return nil

}

func (dn *DropNodeAction) Apply(c *Cluster) {
	if err := dn.Validate(c); err != nil {
		log.Printf("[WARNING] Skipping drop node action. Reason: '%s'", err)
		return
	}

	maxFaultyNodes := GetMaxFaultyNodes(len(c.nodes))
	ticker := time.NewTicker(dn.droppingInterval)
	after := time.After(dn.duration)

	for {
		select {
		case <-ticker.C:
			var runningNodes []*node
			for i := 0; i < dn.dropCount; i++ {
				runningNodes = c.GetRunningNodes()
				log.Printf("%d. BEGIN Running nodes: %v", i+1, runningNodes)

				// Stop a node with some probability
				if rand.Intn(100)+1 > dn.dropProbabilityThreshold {
					nodeToStop := runningNodes[rand.Intn(len(runningNodes))]
					log.Printf("Dropping node '%s'.", nodeToStop)
					c.StopNode(nodeToStop.name)
				}

				// Keep number of running nodes above max faulty nodes count
				stoppedNodes := c.GetStoppedNodes()
				for len(stoppedNodes) > maxFaultyNodes {
					// Randomly choose a node, from the set of stopped nodes to be started again
					nodeToStart := stoppedNodes[rand.Intn(len(stoppedNodes))]
					log.Printf("Starting node '%s'.", nodeToStart)
					nodeToStart.Start()
					stoppedNodes = c.GetStoppedNodes()
				}
				log.Printf("%d. END Running nodes: %v", i+1, c.GetRunningNodes())
			}
		case <-after:
			ticker.Stop()
			log.Println("Done dropping nodes.")
			return
		}
	}
}

// Revert reverts all nodes that are not running
func (dn *DropNodeAction) Revert(c *Cluster) {
	log.Println("Reverting dropped nodes")
	for _, node := range c.GetStoppedNodes() {
		node.Start()
	}
}

// Holds all available pre-defined actions
type actionRepository struct {
	actions []Action
}

func newActionRepository() *actionRepository {
	return &actionRepository{actions: createActions()}
}

func createActions() []Action {
	actions := []Action{
		newDropNodeAction(2, 50, 1*time.Second, 10*time.Second),
		newDropNodeAction(2, 70, 500*time.Millisecond, 10*time.Second),
	}
	return actions
}
