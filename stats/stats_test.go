package stats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMeter_IncrementMessageCount(t *testing.T) {
	stats := NewStats()

	// increment Round Change Message count
	stats.IncrMsgCount(uint32(0))
	// increment Prepepare Message count
	stats.IncrMsgCount(uint32(1))
	stats.IncrMsgCount(uint32(1))
	// increment Commit Message count
	stats.IncrMsgCount(uint32(2))
	// increment Prepare Message count
	stats.IncrMsgCount(uint32(3))
	stats.IncrMsgCount(uint32(3))
	stats.IncrMsgCount(uint32(3))

	// assert Round Changes
	assert.Equal(t, uint64(1), stats.msgCount[uint32(0)])
	// assert Prepepares
	assert.Equal(t, uint64(2), stats.msgCount[uint32(1)])
	// assert Commit Messages
	assert.Equal(t, uint64(1), stats.msgCount[uint32(2)])
	// assert Prepare Message
	assert.Equal(t, uint64(3), stats.msgCount[uint32(3)])
}
