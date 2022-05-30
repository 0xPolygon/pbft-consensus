package stats

import (
	"sync"
	"time"
)

type Stats struct {
	// todo atomic instead of lock?
	lock *sync.Mutex

	numPrePrepareMsg  uint64
	numPrepareMsg     uint64
	numCommitMsg      uint64
	numRoundChangeMsg uint64

	//TODO: duration between states?
	acceptStateDuration      time.Duration
	validateStateDuration    time.Duration
	commitStateDuration      time.Duration
	roundChangeStateDuration time.Duration
}

func NewStats() *Stats {
	return &Stats{
		lock: &sync.Mutex{},
	}
}

func (s *Stats) IncrRoundChangeMsgCount() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.numRoundChangeMsg++
}

func (s *Stats) IncrPrePrepareMsgCount() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.numPrePrepareMsg++
}

func (s *Stats) IncrPrepareMsgCount() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.numPrepareMsg++
}

func (s *Stats) IncrCommitMsgCount() {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.numCommitMsg++
}

func (s *Stats) AcceptState(t time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.acceptStateDuration = time.Since(t)
}

func (s *Stats) ValidateState(t time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.validateStateDuration = time.Since(t)
}

func (s *Stats) RoundChangeState(t time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.roundChangeStateDuration = time.Since(t)
}

func (s *Stats) CommitState(t time.Time) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.commitStateDuration = time.Since(t)
}

func (s *Stats) Stats() *Stats {
	// Allocate a new stats struct
	stats := Stats{}
	s.lock.Lock()
	defer s.lock.Unlock()

	// Copy all the stats
	stats.numPrePrepareMsg = s.numPrePrepareMsg
	stats.numPrepareMsg = s.numPrepareMsg
	stats.numCommitMsg = s.numCommitMsg
	stats.numRoundChangeMsg = s.numRoundChangeMsg

	stats.acceptStateDuration = s.acceptStateDuration
	stats.validateStateDuration = s.validateStateDuration
	stats.commitStateDuration = s.commitStateDuration
	stats.roundChangeStateDuration = s.roundChangeStateDuration

	return &stats
}

// todo reset?
// todo stats per round?

//func (s *Stats) Reset() {
//
//}
