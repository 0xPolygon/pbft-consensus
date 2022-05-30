package stats

import (
	"sync"
	"time"
)

type Stats struct {
	// todo atomic instead of lock?
	lock *sync.Mutex

	round    uint64
	sequence uint64

	msgCount      map[uint64]uint64
	stateDuration map[uint32]time.Duration
}

func NewStats() *Stats {
	return &Stats{
		lock:          &sync.Mutex{},
		msgCount:      make(map[uint64]uint64),
		stateDuration: make(map[uint32]time.Duration),
	}
}

func (s *Stats) SetView(sequence uint64, round uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.sequence = sequence
	s.round = round
}
func (s *Stats) IncrMsgCount(msgType uint64) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.msgCount[msgType]++
}

func (s *Stats) StateDuration(msgType uint32, t time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.stateDuration[msgType] = time.Since(t)
}

func (s *Stats) Stats() Stats {
	// Allocate a new stats struct
	stats := NewStats()
	s.lock.Lock()
	defer s.lock.Unlock()

	for msgType, count := range s.msgCount {
		stats.msgCount[msgType] = count
	}

	for msgType, duration := range s.stateDuration {
		stats.stateDuration[msgType] = duration
	}

	return *stats
}

// todo reset?
// todo stats per round?
func (s *Stats) Reset() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.msgCount = make(map[uint64]uint64)
	s.stateDuration = make(map[uint32]time.Duration)
}
