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
	revertProbabilityThreshold = 20
)

type Runner struct {
	wg               sync.WaitGroup
	cluster          *e2e.Cluster
	availableActions []e2e.FunctionalAction
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

	// TODO: Randomize time interval?
	applyTicker := time.NewTicker(5 * time.Second)
	revertTicker := time.NewTicker(3 * time.Second)
	validationTicker := time.NewTicker(10 * time.Second)
	defer applyTicker.Stop()
	defer revertTicker.Stop()
	defer validationTicker.Stop()

	var reverts []e2e.RevertFunc

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		for {
			select {
			case <-done:
				log.Println("Done with execution")
				return

			case <-applyTicker.C:
				log.Printf("[RUNNER] Applying action.")
				actionIndex := rand.Intn(len(r.availableActions))
				action := r.availableActions[actionIndex]
				if action.CanApply(r.cluster) {
					revertFn := action.Apply(r.cluster)
					reverts = append(reverts, revertFn)
				}

			case <-revertTicker.C:
				log.Printf("[RUNNER] Reverting action. %d revert actions available", len(reverts))
				if len(reverts) == 0 {
					continue
				}

				if shouldRevert() {
					revertIndex := rand.Intn(len(reverts))
					revertFn := reverts[revertIndex]
					reverts = append(reverts[:revertIndex], reverts[revertIndex+1:]...)
					revertFn()
				}

			case <-validationTicker.C:
				log.Printf("[RUNNER] Validating nodes")
				validateNodes(r.cluster)
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
	return revertProbability >= revertProbabilityThreshold
}

func getAvailableActions() []e2e.FunctionalAction {
	// TODO: add more actions here once implemented...
	return []e2e.FunctionalAction{
		&e2e.DropNodeAction{},
	}
}
