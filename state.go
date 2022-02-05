package pbft

import (
	"bytes"
	"fmt"
	"sync/atomic"
	"time"
)

type MsgType int32

const (
	MessageReq_RoundChange MsgType = 0
	MessageReq_Preprepare  MsgType = 1
	MessageReq_Commit      MsgType = 2
	MessageReq_Prepare     MsgType = 3
)

func (m MsgType) String() string {
	switch m {
	case MessageReq_RoundChange:
		return "RoundChange"
	case MessageReq_Preprepare:
		return "Preprepare"
	case MessageReq_Commit:
		return "Commit"
	case MessageReq_Prepare:
		return "Prepare"
	default:
		panic(fmt.Sprintf("BUG: Bad msgtype %d", m))
	}
}

type MessageReq struct {
	// type is the type of the message
	Type MsgType

	// from is the address of the sender
	From NodeID

	// seal is the committed seal for the proposal (only for commit messages)
	Seal []byte

	// view is the view assigned to the message
	View *View

	// hash of the proposal
	Hash []byte

	// proposal is the arbitrary data proposal (relayed only for preprepare messages)
	Proposal []byte
}

func (m *MessageReq) Validate() error {
	// Hash field has to exist for state != RoundStateChange
	if m.Type != MessageReq_RoundChange {
		if m.Hash == nil {
			return fmt.Errorf("hash is empty for type %s", m.Type.String())
		}
	}

	// TODO
	return nil
}

func (m *MessageReq) SetProposal(proposal []byte) {
	m.Proposal = append([]byte{}, proposal...)
}

func (m *MessageReq) Copy() *MessageReq {
	mm := new(MessageReq)
	*mm = *m
	if m.View != nil {
		mm.View = m.View.Copy()
	}
	if m.Proposal != nil {
		mm.SetProposal(m.Proposal)
	}
	if m.Seal != nil {
		mm.Seal = append([]byte{}, m.Seal...)
	}
	return mm
}

type View struct {
	// round is the current round/height being finalized
	Round uint64

	// Sequence is a sequence number inside the round
	Sequence uint64
}

func (v *View) Copy() *View {
	vv := new(View)
	*vv = *v
	return vv
}

func (v *View) String() string {
	return fmt.Sprintf("(Sequence=%d, Round=%d)", v.Sequence, v.Round)
}

func ViewMsg(sequence, round uint64) *View {
	return &View{
		Round:    round,
		Sequence: sequence,
	}
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

type Proposal struct {
	// Data is an arbitrary set of data to approve in consensus
	Data []byte

	// Time is the time to create the proposal
	Time time.Time

	// Hash is the digest of the data to seal
	Hash []byte
}

// Equal compares whether two proposals have the same hash
func (p *Proposal) Equal(pp *Proposal) bool {
	return bytes.Equal(p.Hash, pp.Hash)
}

// Copy makes a copy of the Proposal
func (p *Proposal) Copy() *Proposal {
	pp := new(Proposal)
	*pp = *p

	pp.Data = append([]byte{}, p.Data...)
	pp.Hash = append([]byte{}, p.Hash...)
	return pp
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

	// Locked round signals whether the proposal is locked and in which round.
	// If it is nil, it means that proposal isn't locked.
	lockedRound *uint64

	// Describes whether there has been an error during the computation
	err error
}

// newState creates a new state with reset round messages
func newState() *currentState {
	c := &currentState{}
	c.resetRoundMsgs()

	return c
}

func (c *currentState) IsLocked() bool {
	return c.lockedRound != nil
}

func (c *currentState) lock(round uint64) {
	c.lockedRound = &round
}

func (c *currentState) unlock() {
	c.proposal = nil
	c.lockedRound = nil
}

func (c *currentState) GetSequence() uint64 {
	return c.view.Sequence
}

func (c *currentState) getCommittedSeals() [][]byte {
	committedSeals := [][]byte{}
	for _, commit := range c.committed {
		committedSeals = append(committedSeals, commit.Seal)
	}
	return committedSeals
}

// getState returns the current state
func (c *currentState) getState() PbftState {
	stateAddr := (*uint64)(&c.state)

	return PbftState(atomic.LoadUint64(stateAddr))
}

// setState sets the current state
func (c *currentState) setState(s PbftState) {
	stateAddr := (*uint64)(&c.state)

	atomic.StoreUint64(stateAddr, uint64(s))
}

// MaxFaultyNodes returns the maximum number of allowed faulty nodes (F), based on the current validator set size
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
	addr := NodeID(msg.From)
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

type ValidatorSet interface {
	CalcProposer(round uint64) NodeID
	Includes(id NodeID) bool
	Len() int
}
