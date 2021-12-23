package e2e

import (
	"testing"
	"time"
)

func TestE2E_NodeDrop(t *testing.T) {
	c := newPBFTCluster(t, "node_drop", "ptr", 5)
	c.Start()

	// wait for two heights and stop node 1
	c.WaitForHeight(2, 1*time.Minute, false)

	c.StopNode("ptr_0")
	c.WaitForHeight(15, 1*time.Minute, false, generateNodeNames(1, 4, "ptr_"))
}
