package e2e

import (
	"errors"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

func TestE2E_Partition_MinorityCanValidate(t *testing.T) {
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	nodesFactor := float32(nodesCnt) * 2.0 / 3.0
	limit := int(nodesFactor) + 1 // 2F+1 nodes can Validate
	for i, node := range c.Nodes() {
		if i >= limit {
			node.StartWithFsmFactory(createInvalidFsm)
		} else {
			node.Start()
		}
	}

	names := generateNodeNames(0, limit, "prt_")
	c.WaitForHeight(4, 1*time.Minute, false, names)
}

func TestE2E_Partition_MajorityCantValidate(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Height reached for minority of nodes")
		}
	}()
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	nodesFactor := float32(nodesCnt) * 2.0 / 3.0
	limit := int(nodesFactor) // + 1 removed because 2F+1 nodes is majority
	for i, node := range c.Nodes() {
		if i >= limit {
			node.StartWithFsmFactory(createInvalidFsm)
		} else {
			node.Start()
		}
	}

	names := generateNodeNames(0, limit, "prt_")
	c.WaitForHeight(3, 1*time.Minute, true, names)
}

func createInvalidFsm(n *node) pbft.Backend {
	fsm := &fsmInvalid{fsm: NewFsm(n)}
	return fsm
}

type fsmInvalid struct {
	fsm
}

func (f *fsmInvalid) Validate(proposal []byte) error {
	return errors.New("invalid propsal")
}
