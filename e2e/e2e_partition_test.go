package e2e

import (
	"bufio"
	"math"
	"math/rand"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e/fuzz/types"
	"github.com/stretchr/testify/assert"
)

func Test_SaveAndLoadReplayMessages(t *testing.T) {
	messages := make([]*types.RoundMessage, 2)
	var seal []byte
	rand.Read(seal)

	msgReq := &pbft.MessageReq{
		Type: pbft.MessageReq_Preprepare,
		From: pbft.NodeID("Bura#2"),
		Seal: seal,
		View: &pbft.View{
			Round:    0,
			Sequence: 1,
		},
		Proposal: []byte{0x2, 0x3},
	}

	messages[0] = types.NewRoundMessage(pbft.NodeID("Bura#1"), msgReq)
	messages[1] = types.NewTimeoutMessage(pbft.NodeID("Bura#2"), 2*time.Second)

	filePath := "./dummy.flow"
	f, _ := os.Create(filePath)

	w := bufio.NewWriter(f)
	rawMessages, _ := types.ConvertToByteArrays(messages)
	for i, rawArtifact := range rawMessages {
		w.WriteString(string(rawArtifact))
		if i != len(rawMessages)-1 {
			w.WriteString("\r\n")
		}
	}
	w.Flush()
	f.Close()

	loadedMessages, _ := types.Load(filePath)
	for _, msg := range loadedMessages {
		t.Logf("%+v\r\n", msg)
	}
}

func TestE2E_Partition_OneMajority(t *testing.T) {
	const nodesCnt = 5
	hook := newPartitionTransport(300 * time.Millisecond)

	config := &ClusterConfig{
		Count:  nodesCnt,
		Name:   "majority_partition",
		Prefix: "prt",
	}

	c := NewPBFTCluster(t, config, hook)
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(5, 1*time.Minute)
	assert.NoError(t, err)

	// create two partitions.
	majorityPartition := []string{"prt_0", "prt_1", "prt_2"}
	minorityPartition := []string{"prt_3", "prt_4"}
	hook.Partition(majorityPartition, minorityPartition)

	// only the majority partition will be able to sync
	err = c.WaitForHeight(10, 1*time.Minute, majorityPartition)
	assert.NoError(t, err)

	// the partition with two nodes is stuck
	c.IsStuck(10*time.Second, minorityPartition)

	// reset all partitions
	hook.Reset()

	allNodes := make([]string, len(c.nodes))
	for i, node := range c.Nodes() {
		allNodes[i] = node.name
	}
	// all nodes should be able to sync
	err = c.WaitForHeight(15, 1*time.Minute, allNodes)
	assert.NoError(t, err)
}

func TestE2E_Partition_MajorityCanValidate(t *testing.T) {
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	config := &ClusterConfig{
		Count:  nodesCnt,
		Name:   "majority_partition",
		Prefix: "prt",
	}

	c := NewPBFTCluster(t, config, hook)
	limit := int(math.Floor(nodesCnt*2.0/3.0)) + 1 // 2F+1 nodes can Validate
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name >= "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	names := generateNodeNames(0, limit, "prt_")
	err := c.WaitForHeight(4, 1*time.Minute, names)
	assert.NoError(t, err)
	// restart minority and wait to sync
	for _, node := range c.Nodes() {
		if node.name >= "prt_"+strconv.Itoa(limit) {
			node.Restart()
		}
	}
	err = c.WaitForHeight(4, 1*time.Minute)
	assert.NoError(t, err)
}

func TestE2E_Partition_MajorityCantValidate(t *testing.T) {
	const nodesCnt = 7 // N=3F + 1, F = 2
	hook := newPartitionTransport(300 * time.Millisecond)

	config := &ClusterConfig{
		Count:  nodesCnt,
		Name:   "majority_partition",
		Prefix: "prt",
	}

	c := NewPBFTCluster(t, config, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name < "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	names := generateNodeNames(limit, nodesCnt, "prt_")
	err := c.WaitForHeight(3, 1*time.Minute, names)
	assert.Errorf(t, err, "Height reached for minority of nodes")
}

func TestE2E_Partition_BigMajorityCantValidate(t *testing.T) {
	const nodesCnt = 100
	hook := newPartitionTransport(300 * time.Millisecond)

	config := &ClusterConfig{
		Count:  nodesCnt,
		Name:   "majority_partition",
		Prefix: "prt",
	}

	c := NewPBFTCluster(t, config, hook)
	limit := int(math.Floor(nodesCnt * 2.0 / 3.0)) // + 1 removed because 2F+1 nodes is majority
	for _, node := range c.Nodes() {
		node.setFaultyNode(node.name <= "prt_"+strconv.Itoa(limit))
	}
	c.Start()
	defer c.Stop()
	nodeNames := generateNodeNames(limit, nodesCnt, "prt_")
	err := c.WaitForHeight(8, 1*time.Minute, nodeNames)
	assert.Errorf(t, err, "Height reached for minority of nodes")
}
