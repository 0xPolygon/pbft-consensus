package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/0xPolygon/pbft-consensus/e2e/helper"
)

func TestE2E_NodeDrop(t *testing.T) {
	t.Parallel()
	config := &ClusterConfig{
		Count:        5,
		Name:         "node_drop",
		Prefix:       "ptr",
		RoundTimeout: helper.GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config)
	c.Start()
	// wait for two heights and stop node 1
	err := c.WaitForHeight(2, 3*time.Second)
	assert.NoError(t, err)

	c.StopNode("ptr_0")
	err = c.WaitForHeight(10, 15*time.Second, helper.GenerateNodeNames(1, 4, "ptr_"))
	assert.NoError(t, err)

	// sync dropped node by starting it again
	c.StartNode("ptr_0")
	err = c.WaitForHeight(10, 15*time.Second)
	assert.NoError(t, err)

	c.Stop()
}

func TestE2E_BulkNodeDrop(t *testing.T) {
	t.Parallel()

	clusterCount := 5
	bulkToDrop := 3

	conf := &ClusterConfig{
		Count:        clusterCount,
		Name:         "bulk_node_drop",
		Prefix:       "ptr",
		RoundTimeout: helper.GetPredefinedTimeout(2 * time.Second),
	}
	c := NewPBFTCluster(t, conf)
	c.Start()
	err := c.WaitForHeight(2, 3*time.Second)
	assert.NoError(t, err)

	// drop bulk of nodes from cluster
	dropNodes := helper.GenerateNodeNames(bulkToDrop, clusterCount-1, "ptr_")
	for _, node := range dropNodes {
		c.StopNode(node)
	}
	c.IsStuck(15*time.Second, dropNodes)

	// restart dropped nodes
	for _, node := range dropNodes {
		c.StartNode(node)
	}
	err = c.WaitForHeight(5, 15*time.Second)
	assert.NoError(t, err)

	c.Stop()
}
