package stats

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIncrMsgCount(t *testing.T) {
	stats := NewStats()
	var wg sync.WaitGroup

	roundChange := "RoundChange"
	preprepare := "Preprepare"
	commit := "Commit"
	prepare := "Prepare"

	// increment Round Change Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(roundChange)
	}()
	// increment Prepepare Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(preprepare)
		stats.IncrMsgCount(preprepare)
	}()
	// increment Commit Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(commit)
	}()
	// increment Prepare Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(prepare)
		stats.IncrMsgCount(prepare)
		stats.IncrMsgCount(prepare)
	}()

	wg.Wait()

	// assert Round Changes
	assert.Equal(t, uint64(1), stats.msgCount[roundChange])
	// assert Prepepares
	assert.Equal(t, uint64(2), stats.msgCount[preprepare])
	// assert Commit Messages
	assert.Equal(t, uint64(1), stats.msgCount[commit])
	// assert Prepare Message
	assert.Equal(t, uint64(3), stats.msgCount[prepare])
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
	preprepare := "Preprepare"

	stats.IncrMsgCount(preprepare)
	stats.IncrMsgCount(preprepare)
	stats.Reset()

	assert.Equal(t, uint64(0), stats.msgCount[preprepare])
}
