package e2e

import (
	"testing"
	"time"
)

func TestE2E_NoIssue(t *testing.T) {
	c := newPBFTCluster(t, "noissue", "noissue", 5, newRandomTransport(300*time.Millisecond))
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(10, 1*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
}
