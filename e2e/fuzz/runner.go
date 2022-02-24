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
	defer r.cluster.Stop()
	done := time.After(d)

	// TODO: Randomize time interval?
	applyTicker := time.NewTicker(5 * time.Second)
	revertTicker := time.NewTicker(3 * time.Second)
	validationTicker := time.NewTicker(1 * time.Minute)
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

				if e2e.ShouldApply(revertProbabilityThreshold) {
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
		err := c.WaitForHeight(expectedHeight, 3*time.Minute, runningNodes)
		if err != nil {
			transportHook := c.GetTransportHook()
			if transportHook != nil {
				log.Printf("Cluster partitions: %v\n", transportHook.GetPartitions())
			}
			for _, n := range c.Nodes() {
				log.Printf("Node: %v, running: %v\n", n.GetName(), n.IsRunning())
			}
			panic("Desired height not reached.")
		}
		log.Println("Cluster validation done.")
	} else {
		log.Println("Skipping validation, not enough running nodes for consensus.")
	}
}

// validateCluster checks if there is enough running nodes that can make consensus
func validateCluster(c *e2e.Cluster) ([]string, bool) {
	totalNodesCount := len(c.Nodes())
	var runningNodes []string
	var partitions map[string][]string
	// running nodes in majority partition
	hook := c.GetTransportHook()
	if hook != nil {
		partitions = hook.GetPartitions()
	}

	var majorityPartition []string
	if len(partitions) == 0 {
		// there are no partitions
		for _, n := range c.GetRunningNodes() {
			majorityPartition = append(majorityPartition, n.GetName())
		}
	} else {
		// get partition with the majority of nodes
		// all subsets are the same
		for _, p := range partitions {
			if len(p) > len(majorityPartition) {
				majorityPartition = p
			}
		}
	}

	// loop through running nodes and check if they are in majority partition
	for _, n := range c.GetRunningNodes() {
		if e2e.Contains(majorityPartition, n.GetName()) {
			runningNodes = append(runningNodes, n.GetName())
		}
	}
	stoppedNodesCount := totalNodesCount - len(runningNodes)
	return runningNodes, stoppedNodesCount <= pbft.MaxFaultyNodes(totalNodesCount)
}

func getAvailableActions() []e2e.FunctionalAction {
	return []e2e.FunctionalAction{
		&e2e.DropNodeAction{},
		&e2e.PartitionAction{},
	}
}
