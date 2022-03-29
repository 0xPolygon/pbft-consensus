package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestE2E_NoIssue(t *testing.T) {
	config := &ClusterConfig{
		Count:  5,
		Name:   "noissue",
		Prefix: "noissue",
	}

	c := NewPBFTCluster(t, config, newRandomTransport(300*time.Millisecond))
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(10, 1*time.Minute)
	assert.NoError(t, err)
}
