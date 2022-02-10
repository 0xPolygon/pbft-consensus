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
	Validate(c *Cluster) error
	Apply(c *Cluster)
	Revert(c *Cluster)
}

// This action encapsulates logic for dropping nodes action.
type DropNodeAction struct {
	nodesToDrop      int
	droppingInterval time.Duration
	duration         time.Duration
}

func NewDropNodeAction(nodesToDrop int, droppingInterval, duration time.Duration) *DropNodeAction {
	return &DropNodeAction{
		nodesToDrop:      nodesToDrop,
		droppingInterval: droppingInterval,
		duration:         duration,
	}
}

func (dn *DropNodeAction) Validate(c *Cluster) error {
	nodesCount := len(c.nodes)
	if dn.nodesToDrop >= nodesCount {
		return fmt.Errorf("trying to drop more nodes (%d) than available in the cluster (%d)", dn.nodesToDrop, nodesCount)
	}
	runningNodes := c.GetRunningNodes()
	nodesLeft := len(runningNodes) - dn.nodesToDrop
	if nodesLeft < pbft.MaxFaultyNodes(nodesCount) {
		return fmt.Errorf("dropping %d nodes would jeopardize Byzantine fault tollerancy.\nExpected at least %d nodes to run, but action would leave it to %d",
			dn.nodesToDrop, pbft.MaxFaultyNodes(nodesCount), nodesLeft)
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
	for {
		select {
		case <-ticker.C:
			var runningNodes []*node
			for i := 0; i < dn.nodesToDrop; i++ {
				log.Printf("Running nodes: %v", runningNodes)
				runningNodes = c.GetRunningNodes()
				if len(runningNodes) == 0 {
					ticker.Stop()
					return
				}
				// Add probability to stop a node
				if rand.Intn(100) > 50 {
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

func (dn *DropNodeAction) Revert(c *Cluster) {
	for _, node := range c.nodes {
		if !node.IsRunning() {
			node.Start()
		}
	}
}
