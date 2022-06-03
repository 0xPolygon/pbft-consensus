package stats

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIncrMsgCount(t *testing.T) {
	stats := NewStats()
	var wg sync.WaitGroup

	// increment Round Change Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(uint32(0))
	}()
	// increment Prepepare Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(uint32(1))
		stats.IncrMsgCount(uint32(1))
	}()
	// increment Commit Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(uint32(2))
	}()
	// increment Prepare Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(uint32(3))
		stats.IncrMsgCount(uint32(3))
		stats.IncrMsgCount(uint32(3))
	}()

	wg.Wait()

	// assert Round Changes
	assert.Equal(t, uint64(1), stats.msgCount[uint32(0)])
	// assert Prepepares
	assert.Equal(t, uint64(2), stats.msgCount[uint32(1)])
	// assert Commit Messages
	assert.Equal(t, uint64(1), stats.msgCount[uint32(2)])
	// assert Prepare Message
	assert.Equal(t, uint64(3), stats.msgCount[uint32(3)])
}

func TestSetView(t *testing.T) {
	stats := NewStats()

	stats.SetView(uint64(1), uint64(1))
	// assert Sequence
	assert.Equal(t, uint64(1), stats.sequence)
	// assert Round
	assert.Equal(t, uint64(1), stats.round)
}

func TestReset(t *testing.T) {
	stats := NewStats()

	stats.IncrMsgCount(uint32(1))
	stats.IncrMsgCount(uint32(1))
	stats.Reset()

	assert.Equal(t, uint64(0), stats.msgCount[(uint32(1))])
}
