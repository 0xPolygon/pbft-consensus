package e2e

import (
	"encoding/json"
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

const (
	DropNode       string = "DropNode"
	RevertDropNode string = "RevertDropNode"
	Partition      string = "Partition"
	LastSequence   string = "LastSequence"
)

// MetaData is a struct that holds data about fuzz actions that happened and need to be saved in .flow file
type MetaData struct {
	DataType string `json:"actionType"`
	Data     string `json:"data"`
	Sequence uint64 `json:"sequence"`
	Round    uint64 `json:"round"`
}

// NewMetaData creates a new MetaData object
func NewMetaData(dataType, data string, sequence, round uint64) *MetaData {
	return &MetaData{
		DataType: dataType,
		Data:     data,
		Sequence: sequence,
		Round:    round,
	}
}

// ConvertActionsToByteArrays converts ActionSerializable slice to JSON representation and returns it back as slice of byte arrays
func ConvertActionsToByteArrays(actions []*MetaData) ([][]byte, error) {
	var allRawACtions [][]byte
	for _, message := range actions {
		currentRawAction, err := json.Marshal(message)
		if err != nil {
			return allRawACtions, err
		}
		allRawACtions = append(allRawACtions, currentRawAction)
	}
	return allRawACtions, nil
}

type RevertFunc func()

type FunctionalAction interface {
	CanApply(c *Cluster) bool
	Apply(c *Cluster) RevertFunc
}

// DropNodeAction encapsulates logic for dropping nodes action.
type DropNodeAction struct {
}

func (dn *DropNodeAction) CanApply(c *Cluster) bool {
	runningNodes := len(c.GetRunningNodes())
	if runningNodes <= 0 {
		return false
	}
	maxFaultyNodes := pbft.MaxFaultyNodes(len(c.nodes))
	remainingNodes := runningNodes - 1
	return remainingNodes >= maxFaultyNodes
}

func (dn *DropNodeAction) Apply(c *Cluster) RevertFunc {
	runningNodes := c.GetRunningNodes()
	nodeToStop := runningNodes[rand.Intn(len(runningNodes))]
	log.Printf("Dropping node: '%s'.", nodeToStop)

	c.StopNode(nodeToStop.name)
	view := nodeToStop.GetCurrentView()
	c.replayMessageNotifier.HandleAction(NewMetaData(DropNode, nodeToStop.name, view.Sequence, view.Round))

	return func() {
		log.Printf("Reverting stopped node %v\n", nodeToStop.name)
		nodeToStop.Start()
		c.replayMessageNotifier.HandleAction(NewMetaData(RevertDropNode, nodeToStop.name, c.GetMaxHeight()+1, 0))
	}
}

type PartitionAction struct {
}

func (action *PartitionAction) CanApply(_ *Cluster) bool {
	return true
}

func (action *PartitionAction) Apply(c *Cluster) RevertFunc {
	c.lock.Lock()
	defer c.lock.Unlock()
	hook := newPartitionTransport(500 * time.Millisecond)
	// create 2 partition with random number of nodes
	// minority with less than quorum size nodes and majority with the rest of the nodes
	quorumSize := pbft.QuorumSize(len(c.nodes))

	var minorityPartition []string
	var majorityPartition []string
	minorityPartitionSize := rand.Intn(quorumSize + 1)
	i := 0
	for n := range c.nodes {
		if i < minorityPartitionSize {
			minorityPartition = append(minorityPartition, n)
			i++
		} else {
			majorityPartition = append(majorityPartition, n)
		}
	}
	log.Printf("Partitions ratio %d/%d, [%v], [%v]\n", len(majorityPartition), len(minorityPartition), majorityPartition, minorityPartition)
	hook.Partition(minorityPartition, majorityPartition)

	c.SetHook(hook)

	return func() {
		c.lock.Lock()
		defer c.lock.Unlock()
		log.Println("Reverting partitions.")
		if tHook := c.transport.getHook(); tHook != nil {
			tHook.Reset()
		}
	}
}
