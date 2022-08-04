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
		stats.IncrMsgCount(roundChange, 1)
	}()
	// increment Prepepare Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(preprepare, 3)
		stats.IncrMsgCount(preprepare, 5)
	}()
	// increment Commit Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(commit, 3)
	}()
	// increment Prepare Message count
	wg.Add(1)
	go func() {
		defer wg.Done()
		stats.IncrMsgCount(prepare, 2)
		stats.IncrMsgCount(prepare, 4)
		stats.IncrMsgCount(prepare, 6)
	}()

	wg.Wait()

	// assert Round change messages
	assert.Equal(t, uint64(1), stats.msgCount[roundChange])
	assert.Equal(t, uint64(1), stats.msgVotingPower[roundChange])
	// assert Pre-prepare messages
	assert.Equal(t, uint64(2), stats.msgCount[preprepare])
	assert.Equal(t, uint64(8), stats.msgVotingPower[preprepare])
	// assert Commit messages
	assert.Equal(t, uint64(1), stats.msgCount[commit])
	assert.Equal(t, uint64(3), stats.msgVotingPower[commit])
	// assert Prepare messages
	assert.Equal(t, uint64(3), stats.msgCount[prepare])
	assert.Equal(t, uint64(12), stats.msgVotingPower[prepare])
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

	stats.IncrMsgCount(preprepare, 1)
	stats.IncrMsgCount(preprepare, 1)
	stats.Reset()

	assert.Equal(t, uint64(0), stats.msgCount[preprepare])
	assert.Equal(t, uint64(0), stats.msgVotingPower[preprepare])
}
