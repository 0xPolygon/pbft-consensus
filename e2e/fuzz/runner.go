package fuzz

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

var (
	revertThreshold        = 30
	maxSleepBetweenActions = 10
)

type Runner struct {
	wg               sync.WaitGroup
	cluster          *e2e.Cluster
	availableActions []e2e.Action
	// TODO: Add runningActions slice, which will be randomly & dynamically generated and will contain sequence of actions being executed?
}

func NewRunner(initialNodesCount uint) *Runner {
	return &Runner{
		availableActions: getAvailableActions(),
		cluster:          e2e.NewPBFTCluster(nil, "fuzz_cluster", "NODE", int(initialNodesCount)),
		wg:               sync.WaitGroup{},
	}
}

func (r *Runner) Run(d time.Duration) error {
	r.cluster.Start()
	// TODO: we need to stop the cluster, what about tracer inside cluster stop?
	// defer r.cluster.Stop()
	done := time.After(d)
	actionsApplied := make([]int, 0)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case <-done:
				log.Println("Done with execution")
				return
			default:
				pick := rand.Intn(len(r.availableActions))
				r.availableActions[pick].Apply(r.cluster)
				actionsApplied = append(actionsApplied, pick)
				wait := time.Duration(rand.Intn(maxSleepBetweenActions)) * time.Second
				time.Sleep(wait)

				// keep all un reverted actions
				var notRevertedAction []int
				for _, aIdx := range actionsApplied {
					if shouldRevert() {
						r.availableActions[aIdx].Revert(r.cluster)
					} else {
						notRevertedAction = append(notRevertedAction, aIdx)
					}
				}
				actionsApplied = notRevertedAction
				validateNodes(r.cluster)
				// TODO: Invoke revert of actions which are probabilistically determined to be reverted.
			}
		}
	}()

	r.wg.Wait()
	return nil
}

// validateNodes checks if there is progress on the node height after the scenario run
func validateNodes(c *e2e.Cluster) {
	if runningNodes, ok := validateCluster(c); ok {
		currentHeight := c.GetMaxHeight(runningNodes)
		expectedHeight := currentHeight + 10
		log.Printf("Current height %v and waiting expected %v height.\n", currentHeight, expectedHeight)
		err := c.WaitForHeight(expectedHeight, 5*time.Minute, runningNodes)
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
	totalNodesCount := 0
	var runningNodes []string
	for _, n := range c.Nodes() {
		if n.IsRunning() {
			runningNodes = append(runningNodes, n.GetName())
		}
		totalNodesCount++
	}
	stoppedNodesCount := totalNodesCount - len(runningNodes)
	return runningNodes, stoppedNodesCount <= pbft.MaxFaultyNodes(totalNodesCount)
}

// shouldRevert is used to randomly chose if particular action should be reverted base on the threshold
func shouldRevert() bool {
	revertProbability := rand.Intn(100) + 1
	return revertProbability >= revertThreshold
}

func getAvailableActions() []e2e.Action {
	// TODO: add more actions here once implemented...
	return []e2e.Action{
		&e2e.DropNodeAction{},
	}
}
