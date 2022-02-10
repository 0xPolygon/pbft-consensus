package e2e

import (
	"log"
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
	// TODO: Add Setup() method?
	Apply(c *Cluster)
	Revert(c *Cluster)
}

// This action encapsulates logic for dropping nodes action.
type DropNodeAction struct {
	nodesCount       int
	nodesToDrop      int
	droppingInterval time.Duration
	duration         time.Duration
}

func NewDropNodeAction(nodeCount, nodesToDrop int, droppingInterval, duration time.Duration) *DropNodeAction {
	return &DropNodeAction{
		nodesCount:       nodeCount,
		nodesToDrop:      nodesToDrop,
		droppingInterval: droppingInterval,
		duration:         duration,
	}
}

func (dn *DropNodeAction) Apply(c *Cluster) {
	ticker := time.NewTicker(dn.droppingInterval)
	after := time.After(dn.duration)
	for {
		select {
		case <-ticker.C:
			// TODO: Implement node dropping logic here with some probability
			// cluster.StopNode()
			log.Println("Dropping node.")
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
