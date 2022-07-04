package e2e

import (
	"bytes"
	"fmt"
	"github.com/0xPolygon/pbft-consensus"
	"math/rand"
	"sync"
	"testing"
	"time"
)

// TestE2E_Persistence_Issue tests insertion of different proposal for the same sequence.
// In round 0 node proposer A_0 receives enough commit messages from other nodes and inserts a proposal.
// Node A_3 in round 0 gets locked.
// Node A_2 after receiving enough commit messages fails to insert proposal locally and unlocks the proposal and triggers round change.
// In round 0 nodes A_1 and A_4 do not receive message on time and trigger round change.
// After moving to round 1 proposer A_1 creates a new proposal which then is propagated through the network
// and gets inserted since there is enough commit messages from nodes A_1, A_2 and A_4
// which is different from the proposal inserted in round 0.
func TestE2E_Persistence_Issue(t *testing.T) {
	// init seed so that sequence is predictable
	rand.Seed(0)
	// proposal for round 0
	proposal := []byte{192, 65, 211, 255}
	insertFunc := func(pp *pbft.Proposal) error {
		if bytes.Equal(pp.Data, proposal) {
			return fmt.Errorf("error inserting proposal")
		}
		return nil
	}

	round0 := roundMetadata{
		round: 0,
		routingMap: map[sender]receivers{
			"A_0": {"A_0", "A_1", "A_2", "A_3"},
			"A_1": {"A_1", "A_2", "A_3"},
			"A_2": {"A_0", "A_2"},
			"A_3": {"A_0", "A_2"},
		},
	}
	flowMap := map[uint64]roundMetadata{0: round0}
	transport := newGenericGossipTransport()

	gossipHandler := func(senderId, receiverId pbft.NodeID, msg *pbft.MessageReq) (sent bool) {
		if msg.View.Sequence > 1 || (msg.View.Sequence == 1 && msg.View.Round >= 1) {
			return true
		}
		return transport.shouldGossipBasedOnMsgFlowMap(msg, senderId, receiverId)
	}

	transport.withFlowMap(flowMap).withGossipHandler(gossipHandler)

	config := &ClusterConfig{
		Count:  5,
		Name:   "persistence_issue",
		Prefix: "A",
	}

	c := NewPBFTCluster(t, config, transport)
	// insertFunc fails to insert first proposal for node A_2
	c.nodes["A_2"].insert = insertFunc

	c.Start()
	defer c.Stop()

	timeout := time.After(5 * time.Second)
	wg := sync.WaitGroup{}
	wg.Add(1)
	// TODO test expects that persistence issue exists
	go func() {
		defer wg.Done()
		select {
		case err := <-c.nodes["A_1"].panicCh:
			if err != nil {
				if err != proposalInsertionError {
					t.Fail()
				}
			}
		case <-timeout:
			t.Fail()
		}
	}()
	wg.Wait()
}
