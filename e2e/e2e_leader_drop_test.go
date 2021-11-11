package e2e

import (
	"testing"
	"time"
)

func TestE2E_LeaderDrop(t *testing.T) {
	c := newIBFTCluster(t, "leader_drop", 5)
	c.Start()

	// wait for two blocks and stop node 1
	c.WaitForHeight(2, 1*time.Minute)

	c.StopNode("leader_drop_0")
	c.WaitForHeight(15, 1*time.Minute)
}
