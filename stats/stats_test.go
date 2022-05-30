package stats

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMeter_IncrementMessageCount(t *testing.T) {
	stats := NewStats()
	stats.IncrRoundChangeMsgCount()
	stats.IncrCommitMsgCount()
	stats.IncrPrepareMsgCount()
	stats.IncrPrePrepareMsgCount()

	assert.Equal(t, uint64(1), stats.numRoundChangeMsg)
	assert.Equal(t, uint64(1), stats.numCommitMsg)
	assert.Equal(t, uint64(1), stats.numPrePrepareMsg)
	assert.Equal(t, uint64(1), stats.numPrepareMsg)
}
