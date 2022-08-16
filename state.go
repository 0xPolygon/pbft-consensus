package pbft

import (
	"sync"
	"sync/atomic"
	"time"
)

// state defines the current state object in PBFT
type state struct {
	// mutex enables thread safe access to the state fields
	mutex sync.RWMutex

	// validators represent the current validator set
	validators ValidatorSet

	// state is the current state
	state uint64

	// proposal stores information about the height proposal
	proposal *Proposal

	// proposer is the selected proposer
	proposer NodeID

	// view is a current view
	view *View

	// prepared is a list of prepared messages
	prepared *messages

	// committed is a list of committed messages
	committed *messages

	// roundMessages is a list of round change messages
	roundMessages map[uint64]*messages

	// maxFaultyVotingPower represents max tolerable faulty voting power in order to have Byzantine fault tollerance property satisfied
	maxFaultyVotingPower uint64

	// quorumSize represents minimum accumulated voting power needed to proceed to next PBFT state
	quorumSize uint64

	// locked signals whether the proposal is locked
	locked uint64

	// timeout tracks the time left for this round
	timeoutChan <-chan time.Time

	// err describes whether there has been an error during the computation
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
	maxFaultyVotingPower, quorumSize, err := CalculateQuorum(s.getValidatorSet().VotingPower())
	if err != nil {
		return err
	}
	s.maxFaultyVotingPower = maxFaultyVotingPower
	s.quorumSize = quorumSize
	return nil
}

// getQuorumSize calculates quorum size (namely the number of required messages of some type in order to proceed to the next state in PBFT state machine).
// It is calculated by formula:
// 2 * F + 1, where F denotes maximum count of faulty nodes in order to have Byzantine fault tollerant property satisfied.
func (s *state) getQuorumSize() uint64 {
	return s.quorumSize
}

// getMaxFaultyVotingPower is calculated as at most 1/3 of total voting power of the entire validator set.
func (s *state) getMaxFaultyVotingPower() uint64 {
	return s.maxFaultyVotingPower
}

func (s *state) IsLocked() bool {
	return atomic.LoadUint64(&s.locked) == 1
}

func (s *state) GetSequence() uint64 {
	return s.view.Sequence
}

func (s *state) getCommittedSeals() []CommittedSeal {
	committedSeals := make([]CommittedSeal, 0, len(s.committed.messageMap))
	for nodeId, commit := range s.committed.messageMap {
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

// setValidatorSet sets validator set (thread-safe)
func (s *state) setValidatorSet(validators ValidatorSet) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.validators = validators
}

// getValidatorSet gets validator set (thread-safe)
func (s *state) getValidatorSet() ValidatorSet {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.validators
}

// setProposal sets proposal (thread-safe)
func (s *state) setProposal(proposal *Proposal) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.proposal = proposal
}

// GetProposal gets proposal (thread-safe)
func (s *state) GetProposal() *Proposal {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.proposal
}

// setView sets view (thread-safe)
func (s *state) setView(view *View) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.view = view
}

// getView gets view (thread-safe)
func (s *state) getView() *View {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.view
}

// getErr returns the current error, if any, and consumes it
func (s *state) getErr() error {
	err := s.err
	s.err = nil

	return err
}

// maxRound tries to resolve the round node should fast-track, based on round change messages.
// Quorum size for fast-track higher round is F+1 round change messages (where F denotes max faulty voting power)
func (s *state) maxRound() (maxRound uint64, found bool) {
	for currentRound, messages := range s.roundMessages {
		if messages.getAccumulatedVotingPower() < s.getMaxFaultyVotingPower()+1 {
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
	s.prepared = newMessages()
	s.committed = newMessages()
	s.roundMessages = map[uint64]*messages{}
}

// CalcProposer calculates the proposer and sets it to the state
func (s *state) CalcProposer() {
	s.proposer = s.getValidatorSet().CalcProposer(s.getView().Round)
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

// addRoundChangeMsg adds a ROUND-CHANGE message to the round, and returns the round message size
func (s *state) addRoundChangeMsg(msg *MessageReq) {
	if msg.Type != MessageReq_RoundChange {
		return
	}

	s.addMessage(msg)
}

// addPrepareMsg adds a PREPARE message
func (s *state) addPrepareMsg(msg *MessageReq) {
	if msg.Type != MessageReq_Prepare {
		return
	}

	s.addMessage(msg)
}

// addCommitMsg adds a COMMIT message
func (s *state) addCommitMsg(msg *MessageReq) {
	if msg.Type != MessageReq_Commit {
		return
	}

	s.addMessage(msg)
}

// addMessage adds a new message to one of the following message lists: committed, prepared, roundMessages
func (s *state) addMessage(msg *MessageReq) {
	if !s.getValidatorSet().Includes(msg.From) {
		// only include messages from validators
		return
	}

	votingPower := s.getValidatorSet().VotingPower()[msg.From]
	if msg.Type == MessageReq_Commit {
		s.committed.addMessage(msg, votingPower)
	} else if msg.Type == MessageReq_Prepare {
		s.prepared.addMessage(msg, votingPower)
	} else if msg.Type == MessageReq_RoundChange {
		view := msg.View
		roundChangeMessages, exists := s.roundMessages[view.Round]
		if !exists {
			roundChangeMessages = newMessages()
			s.roundMessages[view.Round] = roundChangeMessages
		}
		roundChangeMessages.addMessage(msg, votingPower)
	}
}

// numPrepared returns the number of messages in the prepared message list
func (s *state) numPrepared() int {
	return s.prepared.length()
}

// numCommitted returns the number of messages in the committed message list
func (s *state) numCommitted() int {
	return s.committed.length()
}

func (s *state) GetCurrentRound() uint64 {
	view := s.getView()
	return atomic.LoadUint64(&view.Round)
}

func (s *state) SetCurrentRound(round uint64) {
	view := s.getView()
	atomic.StoreUint64(&view.Round, round)
}

type messages struct {
	messageMap             map[NodeID]*MessageReq
	accumulatedVotingPower uint64
}

func newMessages() *messages {
	return &messages{
		messageMap:             make(map[NodeID]*MessageReq),
		accumulatedVotingPower: 0,
	}
}

func (m *messages) addMessage(message *MessageReq, votingPower uint64) {
	if _, exists := m.messageMap[message.From]; exists {
		return
	}
	m.messageMap[message.From] = message
	m.accumulatedVotingPower += votingPower
}

func (m messages) getAccumulatedVotingPower() uint64 {
	return m.accumulatedVotingPower
}

func (m messages) length() int {
	return len(m.messageMap)
}
