package e2e

import (
	"testing"
	"time"
)

func TestE2E_NoIssue(t *testing.T) {
	c := newIBFTCluster(t, "noissue", 5, newRandomTransport(300*time.Second))
	c.Start()

	c.WaitForHeight(10, 1*time.Minute)
	c.Stop()
}
