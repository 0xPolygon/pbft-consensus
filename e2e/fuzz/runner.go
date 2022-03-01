package fuzz

import (
	"fmt"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

const (
	revertProbabilityThreshold = 20
	applyTimeInterval          = 5 * time.Second
	revertTimeInterval         = 3 * time.Second
	validationTimeInterval     = 1 * time.Minute
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

func (r *Runner) Run(totalDuration time.Duration) error {
	err := validateDuration(totalDuration)
	if err != nil {
		return err
	}

	r.cluster.Start()
	defer r.cluster.Stop()

	done := time.After(totalDuration)

	applyTicker := time.NewTicker(applyTimeInterval)
	revertTicker := time.NewTicker(revertTimeInterval)
	validationTicker := time.NewTicker(validationTimeInterval)
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

// validateDuration is a sanity check validation which assures that total duration value is larger than all of the predefined time intervals within the runner.
func validateDuration(totalDuration time.Duration) error {
	largestIntervalInMillis := math.Max(float64(applyTimeInterval.Milliseconds()), float64(revertTimeInterval.Milliseconds()))
	largestIntervalInMillis = math.Max(largestIntervalInMillis, float64(validationTimeInterval.Milliseconds()))
	if float64(totalDuration.Milliseconds()) < largestIntervalInMillis {
		largestIntervalInSeconds := largestIntervalInMillis / 1000
		return fmt.Errorf("total duration is less than predefined time interval %vs. Set -duration to at least %vs", largestIntervalInSeconds, largestIntervalInSeconds)
	}
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
				log.Printf("Node: %v, running: %v, locked: %v, proposal: %v\n", n.GetName(), n.IsRunning(), n.IsLocked(), n.GetProposal())
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
	// no partitions, so all running nodes are in the consensus
	if len(partitions) == 0 {
		for _, n := range c.GetRunningNodes() {
			runningNodes = append(runningNodes, n.GetName())
		}

		return runningNodes, true
	}

	// check if there is enough messages coming to particular node
	nodeConnections := make(map[string]int)
	nodesMap := c.GetClusterNodes()
	for node := range partitions {
		nodes := partitions[node]
		// count only connected running nodes
		for _, n := range nodes {
			if nodesMap[n].IsRunning() {
				nodeConnections[node]++
			}
		}
	}
	// check whether there is enough connected nodes
	for k, v := range nodeConnections {
		if v >= c.MinValidNodes() {
			runningNodes = append(runningNodes, k)
		}
	}

	stoppedNodesCount := totalNodesCount - len(runningNodes)
	return runningNodes, stoppedNodesCount <= pbft.MaxFaultyNodes(totalNodesCount)
}

func getAvailableActions() []e2e.FunctionalAction {
	return []e2e.FunctionalAction{
		&e2e.DropNodeAction{},
		&e2e.PartitionAction{},
		&e2e.FlowMapAction{},
	}
}
