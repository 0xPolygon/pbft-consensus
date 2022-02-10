package e2e

import (
	"fmt"
	"log"
	"math/rand"
	"time"
)

type Scenario struct {
	actions []Action
}

func NewScenario() *Scenario {
	return &Scenario{
		actions: make([]Action, 0),
	}
}

func (s *Scenario) AddAction(action Action) *Scenario {
	s.actions = append(s.actions, action)
	return s
}

func (s *Scenario) CleanUp(cluster *Cluster) {
	for _, action := range s.actions {
		action.Revert(cluster)
	}
}

type Action interface {
	Apply(c *Cluster)
	Revert(c *Cluster)
}

// This action encapsulates logic for dropping nodes action.
type DropNodeAction struct {
	dropCount                int
	dropProbabilityThreshold int
	// TODO: Idea: Extract these two fields to another action (called PeriodicAction), which would receive as a parameter another action and would run it?
	droppingInterval time.Duration
	duration         time.Duration
}

func NewDropNodeAction(dropCount, dropProbabilityThreshold int, droppingInterval, duration time.Duration) *DropNodeAction {
	return &DropNodeAction{
		dropCount:                dropCount,
		dropProbabilityThreshold: dropProbabilityThreshold,
		droppingInterval:         droppingInterval,
		duration:                 duration,
	}
}

func (dn *DropNodeAction) Validate(c *Cluster) error {
	nodesCount := len(c.nodes)
	quorumSize := QuorumSize(len(c.nodes))
	if dn.dropCount >= nodesCount {
		return fmt.Errorf("trying to drop more nodes (%d) than available in the cluster (%d)", dn.dropCount, nodesCount)
	}
	runningNodes := c.GetRunningNodes()
	nodesLeft := len(runningNodes) - dn.dropCount
	if nodesLeft < quorumSize {
		return fmt.Errorf("dropping %d nodes would jeopardize minimum node needed for quorum.\nExpected at least %d nodes to run, but action would leave it to %d",
			dn.dropCount, quorumSize, nodesLeft)
	}
	return nil

}

func (dn *DropNodeAction) Apply(c *Cluster) {
	if err := dn.Validate(c); err != nil {
		log.Printf("[WARNING] Skipping drop node action. Reason: '%s'", err)
		return
	}

	quorum := QuorumSize(len(c.nodes))
	ticker := time.NewTicker(dn.droppingInterval)
	after := time.After(dn.duration)

	for {
		select {
		case <-ticker.C:
			var runningNodes []*node
			for i := 0; i < dn.dropCount; i++ {
				runningNodes = c.GetRunningNodes()
				log.Printf("Running nodes: %v", runningNodes)

				// Stop a node with some probability
				if rand.Intn(100)+1 > dn.dropProbabilityThreshold {
					nodeToStop := runningNodes[rand.Intn(len(runningNodes))]
					if quorum > len(c.GetStoppedNodes()) {
						log.Printf("Dropping node '%s'.", nodeToStop)
						c.StopNode(nodeToStop.name)
					}
				}

				runningNodes = c.GetRunningNodes()
				if len(runningNodes) < quorum {
					// Keep number of running nodes above quorum size count
					stoppedNodes := c.GetStoppedNodes()
					if len(stoppedNodes) > 0 {
						// Randomly choose a node, from the set of stopped nodes to be started again
						nodeToStart := stoppedNodes[rand.Intn(len(stoppedNodes))]
						log.Printf("Starting node '%s'.", nodeToStart)
						nodeToStart.Start()
					}
				}
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
