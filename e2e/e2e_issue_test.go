package e2e

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

func TestE2E_Partition_MinorityCanValidate(t *testing.T) {
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	limit := int(math.Floor(nodesCnt*2.0/3.0)) + 1 // 2F+1 nodes can Validate
	for i, node := range c.Nodes() {
		node.StartWithBackendFactory(createBackend(i >= limit))
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
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for i, node := range c.Nodes() {
		node.StartWithBackendFactory(createBackend(i >= limit))
	}

	names := generateNodeNames(0, limit, "prt_")
	c.WaitForHeight(3, 1*time.Minute, true, names)
}

type fsmInvalid struct {
	fsm
}

func (f *fsmInvalid) Validate(proposal []byte) error {
	return errors.New("invalid propsal")
}

func createBackend(invalid bool) func(nn *node) pbft.Backend {
	if invalid {
		return func(nn *node) pbft.Backend {
			return &fsmInvalid{fsm: NewFsm(nn)}
		}
	}
	return func(nn *node) pbft.Backend {
		tmp := NewFsm(nn)
		return &tmp
	}
}
