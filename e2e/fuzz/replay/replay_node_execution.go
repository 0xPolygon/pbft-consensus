package replay

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
)

// replayNodeExecutionHandler handles the simulation of actions and
type replayNodeExecutionHandler struct {
	actionLock            sync.Mutex
	nodesExecutionLock    sync.Mutex
	lastSequencesByNode   map[pbft.NodeID]*e2e.MetaData
	dropNodeActions       map[pbft.NodeID]map[uint64]*e2e.MetaData
	revertDropNodeActions map[pbft.NodeID]map[uint64]*e2e.MetaData
	nodesDoneWithExection map[pbft.NodeID]bool
	dropedNodes           map[pbft.NodeID]uint64
	cluster               *e2e.Cluster
	msgExecutionDone      chan string
	dropNode              chan string
}

// NewNodeExecutionHandler creates a new replayNodeExecutionHandler
func NewNodeExecutionHandler() *replayNodeExecutionHandler {
	return &replayNodeExecutionHandler{
		dropedNodes:      make(map[pbft.NodeID]uint64),
		msgExecutionDone: make(chan string),
		dropNode:         make(chan string),
	}
}

// startActionSimulation starts the cluster and simulates fuzz actions in replay that were saved in meta data file
func (r *replayNodeExecutionHandler) startActionSimulation(cluster *e2e.Cluster) {
	r.cluster = cluster
	revertTicker := time.NewTicker(10 * time.Millisecond)
	nodesCount := len(cluster.GetNodes())
	r.nodesDoneWithExection = make(map[pbft.NodeID]bool, nodesCount)
	stoppedNodes := make(map[string]bool, nodesCount)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		maxHeight := uint64(0)
		for {
			select {
			case <-revertTicker.C:
				currentMaxHeight := cluster.GetMaxHeight() + 1
				if maxHeight < currentMaxHeight {
					maxHeight = currentMaxHeight
				}
				r.checkForNodesToBeRestarted(maxHeight, cluster)
			case nodeDone := <-r.msgExecutionDone:
				cluster.StopNode(nodeDone)
				stoppedNodes[nodeDone] = true
				if len(stoppedNodes) == nodesCount {
					wg.Done()
					return
				}
			case nodeToDrop := <-r.dropNode:
				cluster.StopNode(nodeToDrop)
			default:
				continue
			}
		}
	}()

	cluster.Start()
	wg.Wait()
}

// stopActionSimulation stops the cluster and replay execution
func (r *replayNodeExecutionHandler) stopActionSimulation(cluster *e2e.Cluster) {
	cluster.Stop()
}

// checkIfDoneWithExecution checks if node finished with processing all the messages from .flow file
func (r *replayNodeExecutionHandler) checkIfDoneWithExecution(validatorId pbft.NodeID, sequence, round uint64) bool {
	lastSequence := r.lastSequencesByNode[validatorId]
	if sequence > lastSequence.Sequence ||
		(sequence == lastSequence.Sequence && round == lastSequence.Round) {
		r.nodesExecutionLock.Lock()
		defer r.nodesExecutionLock.Unlock()

		if _, isDone := r.nodesDoneWithExection[validatorId]; !isDone {
			r.nodesDoneWithExection[validatorId] = true
			r.msgExecutionDone <- string(validatorId)
		}
		return true
	}

	return false
}

// checkForNodesToBeRestarted checks if any node need to be restarted when a certain sequence is reached
func (r *replayNodeExecutionHandler) checkForNodesToBeRestarted(sequence uint64, cluster *e2e.Cluster) {
	r.actionLock.Lock()
	defer r.actionLock.Unlock()

	for node, sequenceDroped := range r.dropedNodes {
		if _, hasSequence := r.revertDropNodeActions[node][sequence]; hasSequence && sequence >= sequenceDroped {
			log.Printf("[REPLAY] Restarting node: %v in sequence: %v.\n", node, sequence)
			delete(r.dropedNodes, node)
			cluster.StartNode(string(node))
		}
	}
}

// checkIsTimeout checks if a message is a timeout and triggers the timeout channel of provided validator
func (r *replayNodeExecutionHandler) checkIsTimeout(validatorId pbft.NodeID, msg *pbft.MessageReq, timeoutChannel chan time.Time) bool {
	if isTimeoutMessage(msg) {
		go func() {
			log.Printf("[REPLAY] A timeout occurred in node: %v, sequence: %v, round: %v.\n", validatorId, msg.View.Sequence, msg.View.Round)
			timeoutChannel <- time.Now()
		}()
		return true
	}

	return false
}

// checkIfShouldDrop checks if node should be dropped or stopped in given sequence and round
func (r *replayNodeExecutionHandler) checkIfShouldDrop(validatorId pbft.NodeID, view *pbft.View, timeoutChannel chan time.Time) {
	if _, exists := r.dropNodeActions[validatorId][view.Sequence]; exists {
		log.Printf("[REPLAY] Dropping node: %v in sequence: %v.\n", validatorId, view.Sequence)
		r.nodesExecutionLock.Lock()
		defer r.nodesExecutionLock.Unlock()

		if r.lastSequencesByNode[validatorId].Sequence <= view.Sequence {
			// it is dropped in the last sequence, there is no messages for it after this point, so it is done with execution
			if _, isDone := r.nodesDoneWithExection[validatorId]; !isDone {
				r.nodesDoneWithExection[validatorId] = true
				r.msgExecutionDone <- string(validatorId)
			}
		} else {
			r.dropedNodes[validatorId] = view.Sequence
			r.dropNode <- string(validatorId)
		}
	} else {
		if !r.checkIfDoneWithExecution(validatorId, view.Sequence, view.Round) {
			panic(fmt.Sprintf("node: %v is incorrect state in replay. It should be either dropped or done.", validatorId))
		}
	}
}

// isTimeoutMessage checks if message in .flow file represents a timeout
func isTimeoutMessage(message *pbft.MessageReq) bool {
	return message.Hash == nil && message.Proposal == nil && message.Seal == nil && message.From == ""
}
