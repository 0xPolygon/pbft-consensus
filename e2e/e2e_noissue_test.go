package e2e

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/0xPolygon/pbft-consensus/e2e/helper"
	"github.com/0xPolygon/pbft-consensus/e2e/transport"
)

func TestE2E_NoIssue(t *testing.T) {
	t.Parallel()
	config := &ClusterConfig{
		Count:        5,
		Name:         "noissue",
		Prefix:       "noissue",
		RoundTimeout: helper.GetPredefinedTimeout(2 * time.Second),
	}

	c := NewPBFTCluster(t, config, transport.NewRandom(50*time.Millisecond))
	c.Start()
	defer c.Stop()

	err := c.WaitForHeight(10, 1*time.Minute)
	assert.NoError(t, err)
}
