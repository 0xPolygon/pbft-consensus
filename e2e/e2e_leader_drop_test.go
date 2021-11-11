package e2e

import (
	"testing"
	"time"
)

func TestE2E_NodeDrop(t *testing.T) {
	c := newIBFTCluster(t, "node_drop", "ptr", 5)
	c.Start()

	// wait for two blocks and stop node 1
	c.WaitForHeight(2, 1*time.Minute)

	c.StopNode("ptr_0")
	c.WaitForHeight(15, 1*time.Minute, []string{"ptr_1", "ptr_2", "ptr_3", "ptr_4"})
}
