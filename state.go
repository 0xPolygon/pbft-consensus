package pbft

import (
	"fmt"
	"sync/atomic"
	"time"
)

type View struct {
	// round is the current round/height being finalized
	Round uint64 `json:"round"`

	// Sequence is a sequence number inside the round
	Sequence uint64 `json:"sequence"`
}

func ViewMsg(sequence, round uint64) *View {
	return &View{
		Round:    round,
		Sequence: sequence,
	}
}

func (v *View) Copy() *View {
	vv := new(View)
	*vv = *v
	return vv
}

func (v *View) String() string {
	return fmt.Sprintf("(Sequence=%d, Round=%d)", v.Sequence, v.Round)
}

type NodeID string

type PbftState uint32

// Define the states in PBFT
const (
	AcceptState PbftState = iota
	RoundChangeState
	ValidateState
	CommitState
	SyncState
	DoneState
)

// String returns the string representation of the passed in state
func (i PbftState) String() string {
	switch i {
	case AcceptState:
		return "AcceptState"
	case RoundChangeState:
		return "RoundChangeState"
	case ValidateState:
		return "ValidateState"
	case CommitState:
		return "CommitState"
	case SyncState:
		return "SyncState"
	case DoneState:
		return "DoneState"
	}
	panic(fmt.Sprintf("BUG: Pbft state not found %d", i))
}

type CommittedSeal struct {
	// Signature value
	Signature []byte

	// Node that signed
	NodeID NodeID
}

// currentState defines the current state object in PBFT
type currentState struct {
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

	// List of prepared messages
	prepared map[NodeID]*MessageReq

	// List of committed messages
	committed map[NodeID]*MessageReq

	// List of round change messages
	roundMessages map[uint64]map[NodeID]*MessageReq

	// Locked signals whether the proposal is locked
	locked uint64

	// timeout tracks the time left for this round
	timeoutChan <-chan time.Time

	// Describes whether there has been an error during the computation
	err error
}

// newState creates a new state with reset round messages
func newState() *currentState {
	c := &currentState{
		// this is a default value, it will get reset
		// at every iteration
		timeoutChan: nil,
	}
	c.resetRoundMsgs()

	return c
}

func (c *currentState) IsLocked() bool {
	return atomic.LoadUint64(&c.locked) == 1
}

func (c *currentState) GetSequence() uint64 {
	return c.view.Sequence
}

func (c *currentState) getCommittedSeals() []CommittedSeal {
	committedSeals := make([]CommittedSeal, 0, len(c.committed))
	for nodeId, commit := range c.committed {
		committedSeals = append(committedSeals, CommittedSeal{Signature: commit.Seal, NodeID: nodeId})
	}
	return committedSeals
}

// getState returns the current state
func (c *currentState) getState() PbftState {
	stateAddr := &c.state

	return PbftState(atomic.LoadUint64(stateAddr))
}

// setState sets the current state
func (c *currentState) setState(s PbftState) {
	stateAddr := &c.state

	atomic.StoreUint64(stateAddr, uint64(s))
}

// MaxFaultyNodes returns the maximum number of allowed faulty nodes (F), based on the current validator set
func (c *currentState) MaxFaultyNodes() int {
	return MaxFaultyNodes(c.validators.Len())
}

// NumValid returns the number of required messages
func (c *currentState) NumValid() int {
	// 2 * F + 1
	// + 1 is up to the caller to add
	// the current node tallying the messages will include its own message
	return QuorumSize(c.validators.Len()) - 1
}

// getErr returns the current error, if any, and consumes it
func (c *currentState) getErr() error {
	err := c.err
	c.err = nil

	return err
}

func (c *currentState) maxRound() (maxRound uint64, found bool) {
	num := c.MaxFaultyNodes() + 1

	for currentRound, messages := range c.roundMessages {
		if len(messages) < num {
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
func (c *currentState) resetRoundMsgs() {
	c.prepared = map[NodeID]*MessageReq{}
	c.committed = map[NodeID]*MessageReq{}
	c.roundMessages = map[uint64]map[NodeID]*MessageReq{}
}

// CalcProposer calculates the proposer and sets it to the state
func (c *currentState) CalcProposer() {
	c.proposer = c.validators.CalcProposer(c.view.Round)
}

func (c *currentState) lock() {
	atomic.StoreUint64(&c.locked, 1)
}

func (c *currentState) unlock() {
	c.proposal = nil
	atomic.StoreUint64(&c.locked, 0)
}

// cleanRound deletes the specific round messages
func (c *currentState) cleanRound(round uint64) {
	delete(c.roundMessages, round)
}

// AddRoundMessage adds a message to the round, and returns the round message size
func (c *currentState) AddRoundMessage(msg *MessageReq) int {
	if msg.Type != MessageReq_RoundChange {
		return 0
	}
	c.addMessage(msg)
	return len(c.roundMessages[msg.View.Round])
}

// addPrepared adds a prepared message
func (c *currentState) addPrepared(msg *MessageReq) {
	if msg.Type != MessageReq_Prepare {
		return
	}

	c.addMessage(msg)
}

// addCommitted adds a committed message
func (c *currentState) addCommitted(msg *MessageReq) {
	if msg.Type != MessageReq_Commit {
		return
	}

	c.addMessage(msg)
}

// addMessage adds a new message to one of the following message lists: committed, prepared, roundMessages
func (c *currentState) addMessage(msg *MessageReq) {
	addr := msg.From
	if !c.validators.Includes(addr) {
		// only include messages from validators
		return
	}

	if msg.Type == MessageReq_Commit {
		c.committed[addr] = msg
	} else if msg.Type == MessageReq_Prepare {
		c.prepared[addr] = msg
	} else if msg.Type == MessageReq_RoundChange {
		view := msg.View
		roundMessages, exists := c.roundMessages[view.Round]
		if !exists {
			roundMessages = map[NodeID]*MessageReq{}
			c.roundMessages[view.Round] = roundMessages
		}
		roundMessages[addr] = msg
	}
}

// numPrepared returns the number of messages in the prepared message list
func (c *currentState) numPrepared() int {
	return len(c.prepared)
}

// numCommitted returns the number of messages in the committed message list
func (c *currentState) numCommitted() int {
	return len(c.committed)
}

func (c *currentState) GetCurrentRound() uint64 {
	return atomic.LoadUint64(&c.view.Round)
}

func (c *currentState) SetCurrentRound(round uint64) {
	atomic.StoreUint64(&c.view.Round, round)
}

func (c *currentState) calculateMessagesVotingPower(mp map[NodeID]*MessageReq, votingPower map[NodeID]uint64) uint64 {
	var vp uint64
	for i := range mp {
		vp += votingPower[i]
	}
	return vp
}
