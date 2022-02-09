package e2e

import (
	"log"
	"time"
)

type Scenario struct {
	actions []*Action
}

func NewScenario() *Scenario {
	return &Scenario{
		actions: make([]*Action, 0),
	}
}

func (s *Scenario) AddAction(action *Action) *Scenario {
	s.actions = append(s.actions, action)
	return s
}

func (s *Scenario) CheckProposal() {

}

type Action interface {
	// TODO: Add Setup() method?
	Apply()
	Revert()
}

// This action encapsulates logic for dropping nodes action.
type DropNodeAction struct {
	c                *cluster
	nodesCount       int
	nodesToDrop      int
	droppingInterval time.Duration
	duration         time.Duration
}

func NewDropNodeAction(cluster *cluster, nodeCount, nodesToDrop int, droppingInterval, duration time.Duration) *DropNodeAction {
	return &DropNodeAction{
		c:                cluster,
		nodesCount:       nodeCount,
		nodesToDrop:      nodesToDrop,
		droppingInterval: droppingInterval,
		duration:         duration,
	}
}

func (a *DropNodeAction) Apply() {
	ticker := time.NewTicker(a.droppingInterval)
	after := time.After(a.duration)
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

func (dn *DropNodeAction) Revert() {
	for _, node := range dn.c.nodes {
		if !node.IsRunning() {
			node.Start()
		}
	}
}
