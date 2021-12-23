package pbft

import (
	"context"
	"crypto/sha1"
	"io"
	"io/ioutil"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTransition_ValidateState_Prepare(t *testing.T) {
	t.Skip()

	// we receive enough prepare messages to lock and commit the proposal
	i := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	i.setState(ValidateState)

	i.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	i.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	// repeated message is not included
	i.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	i.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence:    1,
		state:       ValidateState,
		prepareMsgs: 3,
		commitMsgs:  1, // A commit message
		locked:      true,
		outgoing:    1, // A commit message
	})
}

func TestTransition_ValidateState_CommitFastTrack(t *testing.T) {
	t.Skip()

	// we can directly receive the commit messages and fast track to the commit state
	// even when we do not have yet the preprepare messages
	i := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")

	i.setState(ValidateState)
	i.state.view = ViewMsg(1, 0)
	i.state.locked = true

	i.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
	})
	i.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
	})
	i.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
	})
	i.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence:   1,
		commitMsgs: 3,
		outgoing:   1,
		locked:     false, // unlock after commit
	})
}

func TestTransition_AcceptState_ToSyncState(t *testing.T) {
	// we are in AcceptState and we are not in the validators list
	// means that we have been removed as validator, move to sync state
	i := newMockPbft(t, []string{"A", "B", "C", "D"}, "")
	i.setState(AcceptState)

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    SyncState,
	})
}

var (
	mockProposal  = []byte{0x1, 0x2, 0x3}
	mockProposal1 = []byte{0x1, 0x2, 0x3, 0x4}
)

func TestTransition_AcceptState_Proposer_Propose(t *testing.T) {
	// we are in AcceptState and we are the proposer, it needs to:
	// 1. create a proposal
	// 2. wait for the delay
	// 3. send a preprepare message
	// 4. send a prepare message
	// 5. move to ValidateState

	i := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	i.setState(AcceptState)

	i.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now().Add(1 * time.Second),
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		outgoing: 2, // preprepare and prepare
		state:    ValidateState,
	})
}

func TestTransition_AcceptState_Proposer_Locked(t *testing.T) {
	// we are in AcceptState, we are the proposer but the value is locked.
	// it needs to send the locked proposal again
	i := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	i.setState(AcceptState)

	i.state.locked = true
	i.state.proposal = &Proposal{
		Data: mockProposal,
	}

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    ValidateState,
		locked:   true,
		outgoing: 2, // preprepare and prepare
	})
	assert.Equal(t, i.state.proposal.Data, mockProposal)
}

func TestTransition_AcceptState_Validator_VerifyCorrect(t *testing.T) {
	i := newMockPbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	// A sends the message
	i.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal,
		View:     ViewMsg(1, 0),
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    ValidateState,
		outgoing: 1, // prepare
	})
}

func TestTransition_AcceptState_Validator_VerifyFails(t *testing.T) {
	t.Skip("involves validation of hash that is not done yet")

	i := newMockPbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	// A sends the message
	i.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal,
		View:     ViewMsg(1, 0),
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
		err:      errVerificationFailed,
	})
}

func TestTransition_AcceptState_Validator_ProposerInvalid(t *testing.T) {
	i := newMockPbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	// A is the proposer but C sends the propose, we do not fail
	// but wait for timeout to move to roundChange state
	i.emitMsg(&MessageReq{
		From:     "C",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal,
		View:     ViewMsg(1, 0),
	})
	i.forceTimeout()

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
	})
}

func TestTransition_AcceptState_Validator_LockWrong(t *testing.T) {
	// we are a validator and have a locked state in 'proposal1'.
	// We receive an invalid proposal 'proposal2' with different data.

	i := newMockPbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	// locked proposal
	i.state.proposal = &Proposal{
		Data: mockProposal,
	}
	i.state.locked = true

	// emit the wrong locked proposal
	i.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal1,
		View:     ViewMsg(1, 0),
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
		locked:   true,
		err:      errIncorrectLockedProposal,
	})
}

func TestTransition_AcceptState_Validator_LockCorrect(t *testing.T) {
	i := newMockPbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	// locked proposal
	proposal := mockProposal

	i.state.proposal = &Proposal{Data: proposal}
	i.state.locked = true

	i.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: proposal,
		View:     ViewMsg(1, 0),
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    ValidateState,
		locked:   true,
		outgoing: 1, // prepare message
	})
}

func TestTransition_RoundChangeState_CatchupRound(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(RoundChangeState)

	// new messages arrive with round number 2
	m.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_RoundChange,
		View: ViewMsg(1, 2),
	})
	m.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_RoundChange,
		View: ViewMsg(1, 2),
	})
	m.emitMsg(&MessageReq{
		From: "D",
		Type: MessageReq_RoundChange,
		View: ViewMsg(1, 2),
	})
	m.Close()

	// as soon as it starts it will move to round 1 because it has
	// not processed all the messages yet.
	// After it receives 3 Round change messages higher than his own
	// round it will change round again and move to accept
	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		round:    2,
		outgoing: 1, // our new round change
		state:    AcceptState,
	})
}

func TestTransition_RoundChangeState_Timeout(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")

	m.forceTimeout()
	m.setState(RoundChangeState)
	m.Close()

	// increases to round 1 at the beginning of the round and sends
	// one RoundChange message.
	// After the timeout, it increases to round 2 and sends another
	// / RoundChange message.
	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		round:    2,
		outgoing: 2, // two round change messages
		state:    RoundChangeState,
	})
}

func TestTransition_RoundChangeState_WeakCertificate(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C", "D", "E", "F", "G"}, "A")

	m.setState(RoundChangeState)

	// send three roundChange messages which are enough to force a
	// weak change where the client moves to that new round state
	m.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_RoundChange,
		View: ViewMsg(1, 2),
	})
	m.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_RoundChange,
		View: ViewMsg(1, 2),
	})
	m.emitMsg(&MessageReq{
		From: "D",
		Type: MessageReq_RoundChange,
		View: ViewMsg(1, 2),
	})
	m.Close()

	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		round:    2,
		outgoing: 2, // two round change messages (0->1, 1->2 after weak certificate)
		state:    RoundChangeState,
	})
}

func TestTransition_RoundChangeState_ErrStartNewRound(t *testing.T) {
	// if we start a round change because there was an error we start
	// a new round right away
	m := newMockPbft(t, []string{"A", "B"}, "A")
	m.Close()

	m.state.err = errVerificationFailed

	m.setState(RoundChangeState)
	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		round:    1,
		state:    RoundChangeState,
		outgoing: 1,
	})
}

func TestTransition_RoundChangeState_StartNewRound(t *testing.T) {
	// if we start round change due to a state timeout and we are on the
	// correct sequence, we start a new round
	m := newMockPbft(t, []string{"A", "B"}, "A")
	m.Close()

	m.setState(RoundChangeState)
	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		round:    1,
		state:    RoundChangeState,
		outgoing: 1,
	})
}

func TestTransition_RoundChangeState_MaxRound(t *testing.T) {
	// if we start round change due to a state timeout we try to catch up
	// with the highest round seen.
	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.Close()

	m.addMessage(&MessageReq{
		From: "B",
		Type: MessageReq_RoundChange,
		View: &View{
			Round:    10,
			Sequence: 1,
		},
	})

	m.setState(RoundChangeState)
	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		round:    10,
		state:    RoundChangeState,
		outgoing: 1,
	})
}

type mockPbft struct {
	*Pbft

	t        *testing.T
	pool     *testerAccountPool
	respMsg  []*MessageReq
	proposal *Proposal
	sequence uint64
	cancelFn context.CancelFunc
}

func (m *mockPbft) emitMsg(msg *MessageReq) {
	// convert the address from the address pool
	// from := m.pool.get(string(msg.From)).Address()
	// msg.From = from

	m.Pbft.pushMessage(msg)
}

func (m *mockPbft) addMessage(msg *MessageReq) {
	// convert the address from the address pool
	// from := m.pool.get(string(msg.From)).Address()
	// msg.From = from

	m.state.addMessage(msg)
}

func (m *mockPbft) Gossip(msg *MessageReq) error {
	m.respMsg = append(m.respMsg, msg)
	return nil
}

func newMockPbft(t *testing.T, accounts []string, account string) *mockPbft {
	pool := newTesterAccountPool()
	pool.add(accounts...)

	validatorSet := newMockValidatorSet(accounts).(*valString)

	m := &mockPbft{
		t:        t,
		pool:     pool,
		respMsg:  []*MessageReq{},
		sequence: 1, // use by default sequence=1
	}

	backend := &mockB{
		mock:       m,
		validators: validatorSet,
	}

	// initialize the signing account
	var acct *testerAccount
	if account == "" {
		// not in validator set, create a new one (not part of the validator set)
		pool.add("xx")
		acct = pool.get("xx")
	} else {
		acct = pool.get(account)
	}

	var loggerOutput io.Writer
	if os.Getenv("SILENT") == "true" {
		loggerOutput = ioutil.Discard
	} else {
		loggerOutput = os.Stdout
	}

	// initialize pbft
	m.Pbft = New(acct, m, WithLogger(log.New(loggerOutput, "", log.LstdFlags)))
	m.Pbft.SetBackend(backend)

	ctx, cancelFn := context.WithCancel(context.Background())
	m.Pbft.ctx = ctx
	m.cancelFn = cancelFn

	return m
}

func (i *mockPbft) Close() {
	i.cancelFn()
}

func (i *mockPbft) setProposal(p *Proposal) {
	i.proposal = p
}

type expectResult struct {
	state    PbftState
	sequence uint64
	round    uint64
	locked   bool
	err      error

	// num of messages
	prepareMsgs uint64
	commitMsgs  uint64

	// outgoing messages
	outgoing uint64
}

func (m *mockPbft) expect(res expectResult) {
	if sequence := m.state.view.Sequence; sequence != res.sequence {
		m.t.Fatalf("incorrect sequence %d %d", sequence, res.sequence)
	}
	if round := m.state.view.Round; round != res.round {
		m.t.Fatalf("incorrect round %d %d", round, res.round)
	}
	if m.getState() != res.state {
		m.t.Fatalf("incorrect state %s %s", m.getState(), res.state)
	}
	if size := len(m.state.prepared); uint64(size) != res.prepareMsgs {
		m.t.Fatalf("incorrect prepared messages %d %d", size, res.prepareMsgs)
	}
	if size := len(m.state.committed); uint64(size) != res.commitMsgs {
		m.t.Fatalf("incorrect commit messages %d %d", size, res.commitMsgs)
	}
	if m.state.locked != res.locked {
		m.t.Fatalf("incorrect locked %v %v", m.state.locked, res.locked)
	}
	if size := len(m.respMsg); uint64(size) != res.outgoing {
		m.t.Fatalf("incorrect outgoing messages %v %v", size, res.outgoing)
	}
	if m.state.err != res.err {
		m.t.Fatalf("incorrect error %v %v", m.state.err, res.err)
	}
}

type mockB struct {
	mock *mockPbft

	validators *valString
}

func (m *mockB) Hash(p []byte) []byte {
	h := sha1.New()
	h.Write(p)
	return h.Sum(nil)
}

func (m *mockB) BuildProposal() (*Proposal, error) {
	if m.mock.proposal == nil {
		panic("add a proposal in the test")
	}
	return m.mock.proposal, nil
}

func (m *mockB) Height() uint64 {
	return m.mock.sequence
}

func (m *mockB) Validate(proposal []byte) error {
	return nil
}

func (m *mockB) IsStuck(num uint64) (uint64, bool) {
	return 0, false
}

func (m *mockB) Insert(pp *SealedProposal) error {
	// TODO
	return nil
}

func (m *mockB) ValidatorSet() ValidatorSet {
	return m.validators
}

func TestExponentialTimeout(t *testing.T) {
	testCases := []struct {
		description string
		exponent    uint64
		expected    time.Duration
	}{
		{"for round 0 timeout 3s", 0, defaultTimeout + (1 * time.Second)},
		{"for round 1 timeout 4s", 1, defaultTimeout + (2 * time.Second)},
		{"for round 2 timeout 6s", 2, defaultTimeout + (4 * time.Second)},
		{"for round 8 timeout 258s", 8, defaultTimeout + (256 * time.Second)},
		{"for round 9 timeout 300s", 9, 300 * time.Second},
		{"for round 10 timeout 5m", 10, 5 * time.Minute},
		{"for round 34 timeout 5m", 34, 5 * time.Minute},
	}

	for _, test := range testCases {
		t.Run(test.description, func(t *testing.T) {
			ibft := Pbft{state: &currentState{view: &View{Round: test.exponent}}}
			timeout := ibft.exponentialTimeout()
			assert.Equal(t, test.expected, timeout)
		})
	}
}
