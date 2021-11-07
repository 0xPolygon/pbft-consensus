package ibft

import (
	"encoding/hex"
	"fmt"
	"math"
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

type MessageReq struct {
	// type is the type of the message
	Type MsgType

	// from is the address of the sender
	From NodeID

	// seal is the committed seal if message is commit
	Seal []byte

	// view is the view assigned to the message
	View *View

	// hash of the locked block
	Digest string

	// proposal is the rlp encoded block in preprepare messages
	Proposal []byte
}

func (m *MessageReq) SetProposal(b []byte) {
	m.Proposal = append([]byte{}, b...)
}

func (m *MessageReq) Copy() *MessageReq {
	mm := new(MessageReq)
	*mm = *m
	if m.View != nil {
		mm.View = m.View.Copy()
	}
	if m.Proposal != nil {
		mm.Proposal = append([]byte{}, m.Proposal...)
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

func ViewMsg(sequence, round uint64) *View {
	return &View{
		Round:    round,
		Sequence: sequence,
	}
}

type NodeID string

type IbftState uint32

// Define the states in IBFT
const (
	AcceptState IbftState = iota
	RoundChangeState
	ValidateState
	CommitState
	SyncState
)

// String returns the string representation of the passed in state
func (i IbftState) String() string {
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
	}
	panic(fmt.Sprintf("BUG: Ibft state not found %d", i))
}

type Proposal struct {
	// Data is an arbitrary set of data to approve in consensus
	Data []byte

	// Time is the time to create the proposal
	Time time.Time
}

// currentState defines the current state object in IBFT
type currentState struct {
	// validators represent the current validator set
	validators ValidatorSetInterface

	snap *Snapshot

	// state is the current state
	state uint64

	// The proposed block of arbitrary data
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
	locked bool

	// Describes whether there has been an error during the computation
	err error
}

// newState creates a new state with reset round messages
func newState() *currentState {
	c := &currentState{}
	c.resetRoundMsgs()

	return c
}

func (c *currentState) getCommittedSeals() [][]byte {
	committedSeals := [][]byte{}
	for _, commit := range c.committed {
		committedSeals = append(committedSeals, commit.Seal)
	}
	return committedSeals
}

// getState returns the current state
func (c *currentState) getState() IbftState {
	stateAddr := (*uint64)(&c.state)

	return IbftState(atomic.LoadUint64(stateAddr))
}

// setState sets the current state
func (c *currentState) setState(s IbftState) {
	stateAddr := (*uint64)(&c.state)

	atomic.StoreUint64(stateAddr, uint64(s))
}

func (c *currentState) MaxFaultyNodes() int {
	return int(math.Ceil(float64(c.validators.Len())/3)) - 1
}

// NumValid returns the number of required messages
func (c *currentState) NumValid() int {
	return 2 * c.MaxFaultyNodes()
}

// getErr returns the current error, if any, and consumes it
func (c *currentState) getErr() error {
	err := c.err
	c.err = nil

	return err
}

func (c *currentState) maxRound() (maxRound uint64, found bool) {
	num := c.MaxFaultyNodes() + 1

	for k, round := range c.roundMessages {
		if len(round) < num {
			continue
		}
		if maxRound < k {
			maxRound = k
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
	c.locked = true
}

func (c *currentState) unlock() {
	c.proposal = nil
	c.locked = false
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
		if _, ok := c.roundMessages[view.Round]; !ok {
			c.roundMessages[view.Round] = map[NodeID]*MessageReq{}
		}

		c.roundMessages[view.Round][addr] = msg
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

type ValidatorSetInterface interface {
	CalcProposer(round uint64) NodeID
	Includes(id NodeID) bool
	Len() int
}

type ValidatorSet []NodeID

// CalcProposer calculates the address of the next proposer, from the validator set
func (v *ValidatorSet) CalcProposer(round uint64, lastProposer NodeID) NodeID {
	seed := uint64(0)
	if lastProposer == "" {
		seed = round
	} else {
		offset := 0
		if indx := v.Index(lastProposer); indx != -1 {
			offset = indx
		}

		seed = uint64(offset) + round + 1
	}

	pick := seed % uint64(v.Len())

	return (*v)[pick]
}

/*
// Add adds a new address to the validator set
func (v *ValidatorSet) Add(addr NodeID) {
	*v = append(*v, addr)
}

// Del removes an address from the validator set
func (v *ValidatorSet) Del(addr NodeID) {
	for indx, i := range *v {
		if i == addr {
			*v = append((*v)[:indx], (*v)[indx+1:]...)
		}
	}
}
*/

// Len returns the size of the validator set
func (v *ValidatorSet) Len() int {
	return len(*v)
}

/*
// Equal checks if 2 validator sets are equal
func (v *ValidatorSet) Equal(vv *ValidatorSet) bool {
	if len(*v) != len(*vv) {
		return false
	}
	for indx := range *v {
		if (*v)[indx] != (*vv)[indx] {
			return false
		}
	}

	return true
}
*/

// Index returns the index of the passed in address in the validator set.
// Returns -1 if not found
func (v *ValidatorSet) Index(addr NodeID) int {
	for indx, i := range *v {
		if i == addr {
			return indx
		}
	}

	return -1
}

// Includes checks if the address is in the validator set
func (v *ValidatorSet) Includes(addr NodeID) bool {
	return v.Index(addr) != -1
}

// MaxFaultyNodes returns the maximum number of allowed faulty nodes, based on the current validator set
func (v *ValidatorSet) MaxFaultyNodes() int {
	// numberOfValidators / 3
	return int(math.Ceil(float64(len(*v))/3)) - 1
}

// EncodeToHex generates a hex string based on the byte representation, with the '0x' prefix
func EncodeToHex(str []byte) string {
	return "0x" + hex.EncodeToString(str)
}
