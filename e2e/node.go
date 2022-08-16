package e2e

import (
	"context"
	"fmt"
	"log"
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e/helper"
	"github.com/0xPolygon/pbft-consensus/e2e/transport"
)

type node struct {
	// index of node synchronization with the cluster
	localSyncIndex int64

	c *Cluster

	name           string
	pbft           *pbft.Pbft
	cancelFn       context.CancelFunc
	running        uint64
	isShuttingDown uint64

	// validator nodes
	nodes []string

	// indicate if the node is faulty
	faulty uint64
}

func newPBFTNode(name string, clusterConfig *ClusterConfig, nodes []string, trace trace.Tracer, tt *transport.Transport) *node {
	loggerOutput := helper.GetLoggerOutput(name, clusterConfig.LogsDir)

	con := pbft.New(
		pbft.ValidatorKeyMock(name),
		tt,
		pbft.WithTracer(trace),
		pbft.WithLogger(log.New(loggerOutput, "", log.LstdFlags)),
		pbft.WithNotifier(clusterConfig.ReplayMessageNotifier),
		pbft.WithRoundTimeout(clusterConfig.RoundTimeout),
	)

	if clusterConfig.TransportHandler != nil {
		//for replay messages when we do not want to gossip messages
		tt.Register(pbft.NodeID(name), clusterConfig.TransportHandler)
	} else {
		tt.Register(pbft.NodeID(name), func(to pbft.NodeID, msg *pbft.MessageReq) {
			// pipe messages from mock transport to pbft
			con.PushMessage(msg)
			clusterConfig.ReplayMessageNotifier.HandleMessage(to, msg)
		})
	}

	return &node{
		nodes:   nodes,
		name:    name,
		pbft:    con,
		running: 0,
		// set to init index -1 so that zero value is not the same as first index
		localSyncIndex: -1,
	}
}

func (n *node) GetName() string {
	return n.name
}

func (n *node) String() string {
	return n.name
}

// GetNodeHeight returns node height depending on node index
// difference between height and syncIndex is 1
// first inserted proposal is on index 0 with height 1
func (n *node) GetNodeHeight() uint64 {
	return uint64(n.getSyncIndex()) + 1
}

func (n *node) IsLocked() bool {
	return n.pbft.IsLocked()
}

func (n *node) GetProposal() *pbft.Proposal {
	return n.pbft.GetProposal()
}

func (n *node) PushMessageInternal(message *pbft.MessageReq) {
	n.pbft.PushMessageInternal(message)
}

func (n *node) Start() {
	if n.IsRunning() {
		panic(fmt.Errorf("node '%s' is already started", n))
	}

	// create the ctx and the cancelFn
	ctx, cancelFn := context.WithCancel(context.Background())
	n.cancelFn = cancelFn
	atomic.StoreUint64(&n.running, 1)
	go func() {
		defer atomic.StoreUint64(&n.running, 0)
	SYNC:
		_, syncIndex := n.c.syncWithNetwork(n.name)
		n.setSyncIndex(syncIndex)
		for {
			fsm := n.c.createBackend()
			fsm.SetBackendData(n)

			if err := n.pbft.SetBackend(fsm); err != nil {
				panic(err)
			}

			// start the execution
			n.pbft.Run(ctx)
			err := n.c.replayMessageNotifier.SaveState()
			if err != nil {
				log.Printf("[WARNING] Could not write state to file. Reason: %v", err)
			}

			switch n.pbft.GetState() {
			case pbft.SyncState:
				// we need to go back to sync
				goto SYNC
			case pbft.DoneState:
				// everything worked, move to the next iteration
				currentSyncIndex := n.getSyncIndex()
				n.setSyncIndex(currentSyncIndex + 1)
			default:
				// stopped
				return
			}
		}
	}()
}

func (n *node) Stop() {
	if n.isShutDownOngoing() {
		// ignore stop invocation if node is already in a shutting down condition
		return
	}
	if !n.IsRunning() {
		panic(fmt.Errorf("node %s is already stopped", n.name))
	}
	atomic.StoreUint64(&n.isShuttingDown, 1)
	defer atomic.StoreUint64(&n.isShuttingDown, 0)
	n.cancelFn()
	// block until node is running
	for n.IsRunning() {
	}
}

func (n *node) IsRunning() bool {
	return atomic.LoadUint64(&n.running) != 0
}

func (n *node) isShutDownOngoing() bool {
	return atomic.LoadUint64(&n.isShuttingDown) != 0
}

func (n *node) getSyncIndex() int64 {
	return atomic.LoadInt64(&n.localSyncIndex)
}

func (n *node) setSyncIndex(idx int64) {
	atomic.StoreInt64(&n.localSyncIndex, idx)
}

func (n *node) isStuck(num uint64) (uint64, bool) {
	// get max height in the network
	height, _ := n.c.syncWithNetwork(n.name)
	if height > num {
		return height, true
	}
	return 0, false
}

func (n *node) insert(pp *pbft.SealedProposal) error {
	err := n.c.insertFinalProposal(pp)
	if err != nil {
		panic(err)
	}
	return nil
}

// setFaultyNode sets flag indicating that the node should be faulty or not
// 0 is for not being faulty
func (n *node) setFaultyNode(b bool) {
	if b {
		atomic.StoreUint64(&n.faulty, 1)
	} else {
		atomic.StoreUint64(&n.faulty, 0)
	}
}

// isFaulty checks if the node should be faulty or not depending on the stored value
// 0 is for not being faulty
func (n *node) isFaulty() bool {
	return atomic.LoadUint64(&n.faulty) != 0
}

func (n *node) restart() {
	n.Stop()
	n.Start()
}
