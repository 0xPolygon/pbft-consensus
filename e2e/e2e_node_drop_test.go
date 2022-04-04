package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestE2E_NodeDrop(t *testing.T) {
	t.Parallel()
	config := &ClusterConfig{
		Count:        5,
		Name:         "node_drop",
		Prefix:       "ptr",
		RoundTimeout: GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config)
	c.Start()
	// wait for two heights and stop node 1
	err := c.WaitForHeight(2, 3*time.Second)
	assert.NoError(t, err)

	c.StopNode("ptr_0")
	err = c.WaitForHeight(10, 15*time.Second, generateNodeNames(1, 4, "ptr_"))
	assert.NoError(t, err)

	// sync dropped node by starting it again
	c.StartNode("ptr_0")
	err = c.WaitForHeight(10, 15*time.Second)
	assert.NoError(t, err)

	c.Stop()
}
