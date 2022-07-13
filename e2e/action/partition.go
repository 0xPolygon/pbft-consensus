package action

import (
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e"
	"github.com/0xPolygon/pbft-consensus/e2e/transport"
)

type Partition struct {
}

func (action *Partition) CanApply(_ *e2e.Cluster) bool {
	return true
}

func (action *Partition) Apply(c *e2e.Cluster) RevertFunc {
	nodes := c.Nodes()

	hook := transport.NewPartition(500 * time.Millisecond)
	// create 2 partition with random number of nodes
	// minority with less than quorum size nodes and majority with the rest of the nodes
	quorumSize := pbft.QuorumSize(len(nodes))

	var minorityPartition []string
	var majorityPartition []string
	minorityPartitionSize := rand.Intn(quorumSize + 1)
	i := 0
	for _, n := range nodes {
		if i < minorityPartitionSize {
			minorityPartition = append(minorityPartition, n.GetName())
			i++
		} else {
			majorityPartition = append(majorityPartition, n.GetName())
		}
	}
	log.Printf("Partitions ratio %d/%d, [%v], [%v]\n", len(majorityPartition), len(minorityPartition), majorityPartition, minorityPartition)
	hook.Partition(minorityPartition, majorityPartition)

	c.SetHook(hook)

	return func() {
		log.Println("Reverting partitions.")
		if tHook := c.GetTransportHook(); tHook != nil {
			tHook.Reset()
		}
	}
}
