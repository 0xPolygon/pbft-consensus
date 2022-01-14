package e2e

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

func TestMain(m *testing.M) {
	rand.Seed(time.Now().Unix())
	code := m.Run()
	os.Exit(code)
}

func TestE2E_Partition_MinorityCantValidate(t *testing.T) {
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	limit := int(math.Floor(nodesCnt*2.0/3.0)) + 1 // 2F+1 nodes can Validate
	for _, node := range c.Nodes() {
		node.StartWithBackendFactory(createBackend(node.name >= "prt_"+strconv.Itoa(limit)))
	}

	names := generateNodeNames(0, limit, "prt_")
	err := c.WaitForHeight(4, 1*time.Minute, names)
	if err != nil {
		t.Fatal(err)
	}
}

func TestE2E_Partition_MajorityCantValidate(t *testing.T) {
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for _, node := range c.Nodes() {
		node.StartWithBackendFactory(createBackend(node.name < "prt_"+strconv.Itoa(limit)))
	}
	names := generateNodeNames(limit, nodesCnt, "prt_")
	err := c.WaitForHeight(3, 1*time.Minute, names)
	if err == nil {
		t.Fatal("Height reached for minority of nodes")
	}
}

func TestE2E_Partition_BigMajorityCantValidate(t *testing.T) {
	const nodesCnt = 100
	hook := newPartitionTransport(300 * time.Millisecond)

	c := newPBFTCluster(t, "majority_partition", "prt", nodesCnt, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for _, node := range c.Nodes() {
		node.StartWithBackendFactory(createBackend(node.name <= "prt_"+strconv.Itoa(limit)))
	}

	nodeNames := generateNodeNames(limit, nodesCnt, "prt_")
	err := c.WaitForHeight(8, 1*time.Minute, nodeNames)
	if err == nil {
		t.Fatal("Height reached for minority of nodes")
	}
}

type fsmValidationError struct {
	fsm
}

func (f *fsmValidationError) Validate(_ []byte) error {
	return fmt.Errorf("invalid propsal")
}

type fsmInsertError struct {
	fsm
}

func (f *fsmInsertError) Insert(_ *pbft.SealedProposal) error {
	return fmt.Errorf("insert error")
}

func createBackend(invalid bool) func(nn *node) pbft.Backend {
	return func(nn *node) pbft.Backend {
		if invalid {
			switch rand.Intn(2) {
			case 0:
				return &fsmValidationError{fsm: NewFsm(nn)}
			case 1:
				return &fsmInsertError{fsm: NewFsm(nn)}
			}
		}
		tmp := NewFsm(nn)
		return &tmp
	}
}
