package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestE2E_NoIssue(t *testing.T) {
	t.Parallel()
	config := &ClusterConfig{
		Count:        5,
		Name:         "noissue",
		Prefix:       "noissue",
		RoundTimeout: GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config, newRandomTransport(50*time.Millisecond))
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(10, 1*time.Minute)
	assert.NoError(t, err)
}
