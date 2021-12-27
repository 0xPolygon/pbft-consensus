package e2e

import (
	"errors"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

func init() {
	rand.Seed(time.Now().Unix())
}

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

func TestE2E_Partition_BigMajorityCantValidate(t *testing.T) {
	const nodesCnt = 100 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for i, node := range c.Nodes() {
		node.StartWithBackendFactory(createBackend(i >= limit))
	}

	names := generateNodeNames(0, limit, "prt_")
	c.WaitForHeight(15, 2*time.Minute, false, names)
}

type fsmValidationError struct {
	fsm
}

func (f *fsmValidationError) Validate(_ []byte) error {
	return errors.New("invalid propsal")
}

type fsmInsertError struct {
	fsm
}

func (f *fsmInsertError) Insert(_ *pbft.SealedProposal) error {
	return errors.New("insert error")
}

type fsmWrongHash struct {
	fsm
}

func (f *fsmWrongHash) Hash(p []byte) []byte {
	return []byte("")
}

func createBackend(invalid bool) func(nn *node) pbft.Backend {
	return func(nn *node) pbft.Backend {
		if invalid {
			switch rand.Intn(3) {
			case 0:
				return &fsmValidationError{fsm: NewFsm(nn)}
			case 1:
				return &fsmInsertError{fsm: NewFsm(nn)}
			case 2:
				return &fsmWrongHash{fsm: NewFsm(nn)}
			}
		}
		tmp := NewFsm(nn)
		return &tmp
	}
}
