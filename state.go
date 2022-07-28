package pbft

import (
	"sync/atomic"
	"time"
)

// state defines the current state object in PBFT
type state struct {
	// validators represent the current validator set
	validators ValidatorSet

	// state is the current state
	state uint64

	// proposal stores information about the height proposal
	proposal *Proposal

	// The selected proposer
	proposer NodeID

	// Current view
	view *View

	// Previous round
	previousRound uint64

	// List of prepared messages
	prepared messages

	// List of committed messages
	committed messages

	// List of round change messages
	roundMessages map[uint64]messages

	// maxFaultyVotingPower represents max tolerable faulty voting power in order to have Byzantine fault tollerance property satisfied
	maxFaultyVotingPower uint64

	// quorumSize represents minimum accumulated voting power needed to proceed to next PBFT state
	quorumSize uint64

	// Locked signals whether the proposal is locked
	locked uint64

	// timeout tracks the time left for this round
	timeoutChan <-chan time.Time

	// Describes whether there has been an error during the computation
	err error
}

// newState creates a new state with reset round messages
func newState() *state {
	c := &state{
		// this is a default value, it will get reset
		// at every iteration
		timeoutChan: nil,
	}

	c.resetRoundMsgs()

	return c
}

// initializeVotingInfo populates voting information: maximum faulty voting power and quorum size,
// based on the provided voting power map from ValidatorSet
func (s *state) initializeVotingInfo() error {
	maxFaultyVotingPower, quorumSize, err := CalculateQuorum(s.validators.VotingPower())
	if err != nil {
		return err
	}
	s.maxFaultyVotingPower = maxFaultyVotingPower
	s.quorumSize = quorumSize
	return nil
}

// CalculateVotingPower calculates voting power of provided senders
func (s *state) CalculateVotingPower(senders []NodeID) uint64 {
	accumulatedVotingPower := uint64(0)
	for _, nodeId := range senders {
		accumulatedVotingPower += s.validators.VotingPower()[nodeId]
	}
	return accumulatedVotingPower
}

// QuorumSize calculates quorum size (namely the number of required messages of some type in order to proceed to the next state in PBFT state machine).
// It is calculated by formula:
// 2 * F + 1, where F denotes maximum count of faulty nodes in order to have Byzantine fault tollerant property satisfied.
func (s *state) QuorumSize() uint64 {
	return s.quorumSize
}

// MaxFaultyVotingPower is calculated as at most 1/3 of total voting power of the entire validator set.
func (s *state) MaxFaultyVotingPower() uint64 {
	return s.maxFaultyVotingPower
}

func (s *state) IsLocked() bool {
	return atomic.LoadUint64(&s.locked) == 1
}

func (s *state) GetSequence() uint64 {
	return s.view.Sequence
}

func (s *state) getCommittedSeals() []CommittedSeal {
	committedSeals := make([]CommittedSeal, 0, len(s.committed))
	for nodeId, commit := range s.committed {
		committedSeals = append(committedSeals, CommittedSeal{Signature: commit.Seal, NodeID: nodeId})
	}

	return committedSeals
}

// getState returns the current state
func (s *state) getState() State {
	stateAddr := &s.state

	return State(atomic.LoadUint64(stateAddr))
}

// setState sets the current state
func (s *state) setState(st State) {
	stateAddr := &s.state

	atomic.StoreUint64(stateAddr, uint64(st))
}

// getErr returns the current error, if any, and consumes it
func (s *state) getErr() error {
	err := s.err
	s.err = nil

	return err
}

// maxRound tries to resolve the round node should fast-track, based on round change messages.
// Quorum size for round change messages is F+1 (where F denotes max faulty voting power)
func (s *state) maxRound() (maxRound uint64, found bool) {
	for currentRound, messages := range s.roundMessages {
		accumulatedVotingPower := s.CalculateVotingPower(messages.extractNodeIds())
		if accumulatedVotingPower < s.MaxFaultyVotingPower()+1 {
			continue
		}
		if maxRound < currentRound {
			maxRound = currentRound
			found = true
		}
	}

	return
}

// resetRoundMsgs resets the prepared, committed and round messages in the current state
func (s *state) resetRoundMsgs() {
	s.prepared = map[NodeID]*MessageReq{}
	s.committed = map[NodeID]*MessageReq{}
	s.roundMessages = map[uint64]messages{}
}

// CalcProposer calculates the proposer and sets it to the state
func (s *state) CalcProposer() {
	s.proposer = s.validators.CalcProposer(s.view.Round)
}

func (s *state) lock() {
	atomic.StoreUint64(&s.locked, 1)
}

func (s *state) unlock() {
	s.proposal = nil

	atomic.StoreUint64(&s.locked, 0)
}

// cleanRound deletes the specific round messages
func (s *state) cleanRound(round uint64) {
	delete(s.roundMessages, round)
}

// AddRoundMessage adds a message to the round, and returns the round message size
func (s *state) AddRoundMessage(msg *MessageReq) int {
	if msg.Type != MessageReq_RoundChange {
		return 0
	}

	s.addMessage(msg)

	return len(s.roundMessages[msg.View.Round])
}

// addPrepared adds a prepared message
func (s *state) addPrepared(msg *MessageReq) {
	if msg.Type != MessageReq_Prepare {
		return
	}

	s.addMessage(msg)
}

// addCommitted adds a committed message
func (s *state) addCommitted(msg *MessageReq) {
	if msg.Type != MessageReq_Commit {
		return
	}

	s.addMessage(msg)
}

// addMessage adds a new message to one of the following message lists: committed, prepared, roundMessages
func (s *state) addMessage(msg *MessageReq) {
	addr := msg.From
	if !s.validators.Includes(addr) {
		// only include messages from validators
		return
	}

	if msg.Type == MessageReq_Commit {
		s.committed[addr] = msg
	} else if msg.Type == MessageReq_Prepare {
		s.prepared[addr] = msg
	} else if msg.Type == MessageReq_RoundChange {
		view := msg.View
		roundMessages, exists := s.roundMessages[view.Round]
		if !exists {
			roundMessages = map[NodeID]*MessageReq{}
			s.roundMessages[view.Round] = roundMessages
		}
		roundMessages[addr] = msg
	}
}

// numPrepared returns the number of messages in the prepared message list
func (s *state) numPrepared() int {
	return len(s.prepared)
}

// numCommitted returns the number of messages in the committed message list
func (s *state) numCommitted() int {
	return len(s.committed)
}

func (s *state) GetCurrentRound() uint64 {
	return atomic.LoadUint64(&s.view.Round)
}

func (s *state) GetPreviousRound() uint64 {
	return atomic.LoadUint64(&s.previousRound)
}

func (s *state) SetCurrentRound(round uint64) {
	atomic.StoreUint64(&s.previousRound, s.view.Round)
	atomic.StoreUint64(&s.view.Round, round)
}

type messages map[NodeID]*MessageReq

// extractNodeIds returns slice of message senders
func (m messages) extractNodeIds() []NodeID {
	nodeIds := make([]NodeID, len(m))
	i := 0
	for nodeId := range m {
		nodeIds[i] = nodeId
		i++
	}
	return nodeIds
}
