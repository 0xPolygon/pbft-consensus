package fuzz

import (
	"context"
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

func Setup(initialNodesCount uint) *Runner {
	// example of DropNodeAction
	dn := e2e.NewDropNodeAction(5, 2, 1*time.Second, 10*time.Second)
	return &Runner{
		allActions: []e2e.Action{dn},
		scenarios:  []*e2e.Scenario{e2e.NewScenario()},
		cluster:    e2e.NewPBFTCluster(nil, "fuzz_cluster", "NODE", int(initialNodesCount)),
		wg:         sync.WaitGroup{},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	r.cluster.Start()
	defer r.cluster.Stop()

	r.wg.Add(1)
	var height uint64
	go func(ctx context.Context) {
		// TODO: select test case and validate the end state against the cluster
		for {
			select {
			case <-ctx.Done():
				log.Println("Done executing")
				r.wg.Done()
				return
			default:
				// TODO: Loop by scenarios and extract actions.
				// Scenarios should be somehow created by indexing actions array (randomize scenarios picking?)
				// Are scenarios predefined or dynamically created (in a random manner)?

				r.allActions[getActionIndex()].Apply(r.cluster)
			}

			// TODO: Check should we have proposal built and do we have it built
			if runningNodes, ok := validateCluster(r.cluster); ok {
				// TODO: get height after validation so that there is a progress between 2 validations
				height += 5
				err := r.cluster.WaitForHeight(height, 1*time.Minute, runningNodes)
				if err != nil {
					panic("TODO")
				}
				log.Println("TODO: Cluster validation done")
			} else {
				log.Println("TODO: Not enough running nodes for consensus.")
			}
			// TODO: Loop through scenarios and invoke scenario.CleanUp(r.cluster)
		}
	}(ctx)

	r.wg.Wait()
	return nil
}

func getActionIndex() int {
	return 0
}

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
	nodesCnt := len(c.Nodes())
	return runningNodes, pbft.MaxFaultyNodes(nodesCnt) >= stoppedNodes
}
