package e2e

import (
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus"
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
	// TODO: Add Setup() method?
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
	if dn.dropCount >= nodesCount {
		return fmt.Errorf("trying to drop more nodes (%d) than available in the cluster (%d)", dn.dropCount, nodesCount)
	}
	runningNodes := c.GetRunningNodes()
	nodesLeft := len(runningNodes) - dn.dropCount
	if nodesLeft < pbft.MaxFaultyNodes(nodesCount) {
		return fmt.Errorf("dropping %d nodes would jeopardize Byzantine fault tollerancy.\nExpected at least %d nodes to run, but action would leave it to %d",
			dn.dropCount, pbft.MaxFaultyNodes(nodesCount), nodesLeft)
	}
	return nil
}

func (dn *DropNodeAction) Apply(c *Cluster) {
	if err := dn.Validate(c); err != nil {
		log.Printf("[WARNING] Skipping drop node action. Reason: '%s'", err)
		return
	}

	ticker := time.NewTicker(dn.droppingInterval)
	after := time.After(dn.duration)

	// TODO: extract calculation
	maxFaultyNodes := (len(c.nodes) - 1) / 3
	for {
		select {
		case <-ticker.C:
			var runningNodes []*node
			for i := 0; i < dn.dropCount; i++ {
				runningNodes = c.GetRunningNodes()
				log.Printf("Running nodes: %v", runningNodes)
				if len(runningNodes) <= maxFaultyNodes {
					// It is necessary to maintain number of running nodes above of max faulty nodes count
					stoppedNodes := c.GetStoppedNodes()
					if len(stoppedNodes) > 0 {
						nodeToStart := stoppedNodes[rand.Intn(len(stoppedNodes))]
						nodeToStart.Start()
					}
				}
				// Add probability to stop a node
				if rand.Intn(100)+1 > dn.dropProbabilityThreshold {
					nodeToStop := runningNodes[rand.Intn(len(runningNodes))]
					log.Printf("Dropping node '%s'.", nodeToStop.name)
					c.StopNode(nodeToStop.name)
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
	for _, node := range c.nodes {
		if !node.IsRunning() {
			node.Start()
		}
	}
}
