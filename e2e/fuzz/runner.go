package fuzz

import (
	"log"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

type Runner struct {
	wg         sync.WaitGroup
	cluster    *e2e.Cluster
	allActions []e2e.Action
	scenarios  []*e2e.Scenario
}

func NewRunner(initialNodesCount uint) *Runner {
	// example of DropNodeAction
	dn := e2e.NewDropNodeAction(2, 50, 1*time.Second, 10*time.Second)
	return &Runner{
		allActions: []e2e.Action{dn},
		scenarios:  []*e2e.Scenario{e2e.NewScenario()}, // todo not used?
		cluster:    e2e.NewPBFTCluster(nil, "fuzz_cluster", "NODE", int(initialNodesCount)),
		wg:         sync.WaitGroup{},
	}
}

func (r *Runner) Run(d time.Duration) error {
	r.cluster.Start()
	defer r.cluster.Stop()
	done := time.After(d) // don't run forever/ ctx maybe?

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case <-done:
				log.Println("Done with execution")
				return
			default:
				// TODO: Loop by scenarios and extract actions.
				// Scenarios should be somehow created by indexing actions array (randomize scenarios picking?)
				// Are scenarios predefined or dynamically created (in a random manner)?
				// todo loop through actions with some sleep?
				r.allActions[getActionIndex()].Apply(r.cluster)
			}

			validateNodes(r.cluster)
			r.allActions[getActionIndex()].Revert(r.cluster)
			// TODO: Loop through scenarios and invoke scenario.CleanUp(r.cluster)
		}
	}()

	r.wg.Wait()
	return nil
}

func getActionIndex() int {
	return 0
}

// validateNodes checks if there is progress on the node height after the scenario run
func validateNodes(c *e2e.Cluster) {
	if runningNodes, ok := validateCluster(c); ok {
		currentHeight := c.GetMaxHeight(runningNodes)
		expectedHeight := currentHeight + 10
		log.Printf("Current height %v and waiting expected %v height.\n", currentHeight, expectedHeight)
		err := c.WaitForHeight(expectedHeight, 1*time.Minute, runningNodes)
		if err != nil {
			panic("Desired height not reached.")
		}
		log.Println("Cluster validation done.")
	} else {
		log.Println("Skipping validation, not enough running nodes for consensus.")
	}
}

// validateCluster checks wheter there is enough running nodes that can make consensus
func validateCluster(c *e2e.Cluster) ([]string, bool) {
	stoppedNodes := 0
	var runningNodes []string
	for _, n := range c.Nodes() {
		if n.IsRunning() {
			runningNodes = append(runningNodes, n.GetName())
		} else {
			stoppedNodes++
		}
	}
	return runningNodes, pbft.MaxFaultyNodes(len(c.Nodes())) >= stoppedNodes
}
