package fuzz

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e"
)

type Runner struct {
	wg sync.WaitGroup

	allActions []e2e.Action
	scenarios  []*e2e.Scenario
}

// example of scenario
var dn *e2e.DropNodeAction

func Setup(initialNodesCount uint) *Runner {
	// TODO: Create a cluster with a given initialNodesCount booted up
	cluster := e2e.NewPBFTCluster(nil, "fuzz_cluster", "A", int(initialNodesCount))

	dn = e2e.NewDropNodeAction(cluster, 5, 2, 1*time.Second, 10*time.Second)
	return &Runner{
		allActions: []e2e.Action{dn},
		scenarios:  []*e2e.Scenario{e2e.NewScenario()},
	}
}

func (r *Runner) Run(ctx context.Context) error {
	r.wg = sync.WaitGroup{}
	r.wg.Add(1)
	go func(ctx context.Context) {
		// TODO: select test case and validate the end state against the cluster
		for {
			select {
			case <-ctx.Done():
				log.Println("Done executing")
				r.wg.Done()
				return
			default:
				// TODO: scenarios should be somehow created by indexing actions array (randomize scenarios picking?)
				// Are scenarios predefined or dynamically created (in a random manner)?

				r.allActions[getActionIndex()].Apply()
			}
			// TODO: Check should we have proposal built and do we have it built
		}
	}(ctx)

	r.wg.Wait()
	return nil
}

func getActionIndex() int {
	return 0
}
