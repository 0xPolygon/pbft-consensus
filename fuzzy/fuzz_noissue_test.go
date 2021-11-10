package fuzzy

import (
	"testing"
	"time"
)

func TestFuzz_NoIssue(t *testing.T) {
	c := newIBFTCluster(t, "noissue", 5, newRandomTransport(1*time.Second))
	c.Start()

	c.WaitForHeight(10, 1*time.Minute)
	c.Stop()
}
