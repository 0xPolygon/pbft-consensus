package fuzz

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/0xPolygon/pbft-consensus/e2e/helper"
	"github.com/0xPolygon/pbft-consensus/e2e/notifier"
)

var (
	revertProbabilityThreshold = 20
	waitForHeightTimeInterval  = 10 * time.Minute
)

type runner struct {
	wg               sync.WaitGroup
	cluster          *e2e.Cluster
	availableActions []Action
}

// newRunner is the constructor of runner
func newRunner(initialNodesCount uint, replayMessageNotifier notifier.Notifier) *runner {
	config := &e2e.ClusterConfig{
		Count:                 int(initialNodesCount),
		Name:                  "fuzz_cluster",
		Prefix:                "NODE",
		ReplayMessageNotifier: replayMessageNotifier,
	}

	return &runner{
		availableActions: getAvailableActions(),
		cluster:          e2e.NewPBFTCluster(nil, config),
	}
}

func (r *runner) run(d time.Duration) {
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

	var reverts []RevertFunc

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
				actionIndex := rand.Intn(len(r.availableActions)) //nolint:golint,gosec
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

				if helper.ShouldApply(revertProbabilityThreshold) {
					revertIndex := rand.Intn(len(reverts)) //nolint:golint,gosec
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
}

// validateNodes checks if there is progress on the node height after the scenario run
func validateNodes(c *e2e.Cluster) {
	if runningNodes, ok := validateCluster(c); ok {
		currentHeight := c.GetMaxHeight(runningNodes)
		expectedHeight := currentHeight + 10
		log.Printf("Running nodes: %v. Current height: %v and waiting expected: %v height.\n", runningNodes, currentHeight, expectedHeight)
		err := c.WaitForHeight(expectedHeight, waitForHeightTimeInterval, runningNodes)
		if err != nil {
			transportHook := c.GetTransportHook()
			if transportHook != nil {
				log.Printf("Cluster partitions: %v\n", transportHook.GetPartitions())
			}
			for _, n := range c.Nodes() {
				log.Printf("Node: %v, running: %v, locked: %v, height: %v, proposal: %v\n", n.GetName(), n.IsRunning(), n.IsLocked(), n.GetNodeHeight(), n.GetProposal())
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
		if helper.Contains(majorityPartition, n.GetName()) {
			runningNodes = append(runningNodes, n.GetName())
		}
	}
	quorumSize, err := c.QuorumSize()
	var enoughRunningNodes bool
	if err != nil {
		enoughRunningNodes = false
		log.Printf("[ERROR] failed to validate cluster. Error: %v", err)
	} else {
		enoughRunningNodes = len(runningNodes) >= int(quorumSize)
	}
	return runningNodes, enoughRunningNodes
}
