package fuzzy

import (
	"testing"
	"time"
)

func TestFuzz_NoIssue(t *testing.T) {
	c := newIBFTCluster(t, "noissue", 5)
	c.Start()

	c.WaitForHeight(10, 1*time.Minute)
	c.Stop()
}
