package pbft

import (
	"bytes"
	"container/heap"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	mockProposal  = []byte{0x1, 0x2, 0x3}
	mockProposal1 = []byte{0x1, 0x2, 0x3, 0x4}
)

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

func TestTransition_AcceptState_Proposer_FailedBuildProposal(t *testing.T) {
	buildProposalFunc := func() (*Proposal, error) {
		return nil, errors.New("failed to build a proposal")
	}

	validatorIds := []string{"A", "B", "C"}
	backend := newMockBackend(validatorIds, nil, buildProposalFunc, nil, nil)

	m := newMockPbft(t, validatorIds, "A", backend)
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

	var buf bytes.Buffer
	m.logger.SetOutput(&buf)
	defer func() {
		m.logger.SetOutput(getDefaultLoggerOutput())
	}()

	// Prepare messages
	m.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})

	go func() {
		for {
			if m.GetState() == RoundChangeState {
				m.cancelFn()
				return
			}
		}
	}()

	m.Run(m.ctx)
	m.logger.Println(buf.String())

	assert.Contains(t, buf.String(), "[ERROR] failed to build proposal", "build proposal did not fail")
	assert.True(t, m.IsState(RoundChangeState))
}

func TestTransition_AcceptState_Proposer_Cancellation(t *testing.T) {
	// We are in the AcceptState, we are the proposer.
	// Cancellation occurs, so we are checking whether we are still in AcceptState.
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(AcceptState)
	m.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now().Add(time.Second),
	})

	go func() {
		m.cancelFn()
	}()

	m.runCycle(m.ctx)
	assert.True(t, m.IsState(AcceptState))
}

func TestTransition_AcceptState_NonProposer_Cancellation(t *testing.T) {
	// We are in the AcceptState, we are non proposer node.
	// Cancellation occurs, so we are checking whether we are still in AcceptState.
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "D")
	m.Pbft.state.proposer = "A"

	// mock round timeout
	m.roundTimeout = func(u uint64) time.Duration { return time.Millisecond }

	m.setState(AcceptState)
	m.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now().Add(time.Second),
	})

	go func() {
		m.cancelFn()
	}()

	m.runCycle(context.Background())
	assert.True(t, m.IsState(AcceptState))
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
	i.state.lock()

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

func TestTransition_AcceptState_Validate_ProposalFail(t *testing.T) {
	validateProposalFunc := func(proposal []byte) error {
		return errors.New("failed to validate a proposal")
	}

	validatorIds := []string{"A", "B", "C"}
	backend := newMockBackend(validatorIds, nil, nil, validateProposalFunc, nil)

	m := newMockPbft(t, validatorIds, "C", backend)
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

	m.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now(),
	})

	var buf bytes.Buffer
	m.logger.SetOutput(&buf)
	defer func() {
		m.logger.SetOutput(getDefaultLoggerOutput())
	}()

	// Prepare messages
	m.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Preprepare,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Preprepare,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Preprepare,
		View: ViewMsg(1, 0),
	})

	go func() {
		for {
			if m.GetState() == RoundChangeState {
				m.cancelFn()
				return
			}
		}
	}()

	m.Run(m.ctx)
	t.Log(buf.String())

	assert.Contains(t, buf.String(), "[ERROR] failed to validate proposal", "validate proposal did not fail")
	assert.True(t, m.IsState(RoundChangeState))
}

func TestTransition_AcceptState_NonValidatorNode(t *testing.T) {
	// Node sending a messages isn't among validator set, so state machine should set state to SyncState
	m := newMockPbft(t, []string{"A", "B", "C"}, "")
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)
	m.runCycle(context.Background())

	m.expect(expectResult{
		state:    SyncState,
		sequence: 1,
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

func TestTransition_RoundChangeState_Stuck(t *testing.T) {
	isStuckFn := func(num uint64) (uint64, bool) {
		return 0, true
	}

	validatorIds := []string{"A", "B", "C"}
	mockBackend := newMockBackend(validatorIds, nil, nil, nil, isStuckFn)

	m := newMockPbft(t, validatorIds, "A", mockBackend)
	m.SetState(RoundChangeState)

	m.runCycle(context.Background())
	assert.True(t, m.IsState(SyncState))
}

func TestTransition_ValidateState_MoveToCommitState(t *testing.T) {
	// we receive enough prepare messages to lock and commit the proposal
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(ValidateState)
	m.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now().Add(1 * time.Second),
	})

	// Prepare messages
	m.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	// repeated message is not included
	m.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})

	// Commit messages
	m.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "D",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
	})

	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence:    1,
		state:       CommitState,
		prepareMsgs: 3,
		commitMsgs:  3, // Commit messages (A proposer sent commit via state machine loop, C and D sent commit via emit message)
		locked:      true,
		outgoing:    1, // A commit message
	})
}

func TestTransition_ValidateState_MoveToRoundChangeState(t *testing.T) {
	// No messages are sent, so we are changing state to round change state and jumping out of the state machine loop
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(ValidateState)

	m.runCycle(context.Background())

	assert.True(t, m.IsState(RoundChangeState))
}

func TestTransition_ValidateState_WrongMessageType(t *testing.T) {
	// Send wrong message type within ValidateState and asssure it panics
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(ValidateState)

	// Create preprepare message and push it to validate state message queue
	msg := &MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal,
		View:     ViewMsg(1, 0),
	}
	heap.Push(&m.msgQueue.validateStateQueue, msg)
	assert.PanicsWithError(t, "BUG: Unexpected message type: Preprepare in ValidateState", func() { m.runCycle(context.Background()) })
}

func TestTransition_ValidateState_DiscardMessage(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B"}, "A")
	m.setState(ValidateState)
	m.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now().Add(1 * time.Second),
	})
	m.state.view = ViewMsg(1, 2)

	// Send message from the past (it should be discarded)
	m.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 1),
	})
	// Send future message
	m.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(2, 3),
	})

	m.runCycle(context.Background())
	m.expect(expectResult{
		state:       RoundChangeState,
		round:       2,
		sequence:    1,
		prepareMsgs: 0,
		commitMsgs:  0,
		outgoing:    0})
}

func TestTransition_CommitState_DoneState(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.state.view = ViewMsg(1, 0)
	m.state.proposer = "A"
	m.setState(CommitState)

	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		state:    DoneState,
	})
}

func TestTransition_CommitState_RoundChange(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.state.view = ViewMsg(1, 0)
	m.setState(CommitState)

	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
		err:      errFailedToInsertProposal,
	})
	assert.True(t, m.IsState(RoundChangeState))
}

func TestExponentialTimeout(t *testing.T) {
	testCases := []struct {
		description string
		round       uint64
		expected    time.Duration
	}{
		{"for round 0", 0, defaultTimeout + (1 * time.Second)},
		{"for round 1", 1, defaultTimeout + (2 * time.Second)},
		{"for round 2", 2, defaultTimeout + (4 * time.Second)},
		{"for round 8", 8, defaultTimeout + (256 * time.Second)},
		{"for round 9", 9, maxTimeout},
		{"for round 10", 10, maxTimeout},
		{"for round 34", 34, maxTimeout},
	}

	for _, tc := range testCases {
		tc := tc // rebind tc into this lexical scope
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			timeout := exponentialTimeout(tc.round)
			require.Equal(t, tc.expected, timeout, fmt.Sprintf("timeout should be %s", tc.expected))
		})
	}
}

func TestDoneState_RunCycle_Panics(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.state.view = ViewMsg(1, 0)
	m.SetState(DoneState)

	assert.Panics(t, func() { m.runCycle(context.Background()) })
}

func TestPbft_Run(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.state.view = ViewMsg(1, 0)
	m.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now(),
	})

	// Prepare messages
	m.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	m.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})

	// Jump out from a state machine loop straight away
	ch := make(chan struct{})
	go func() {
		close(ch)
		for {
			if m.getState() == AcceptState {
				m.cancelFn()
				return
			}
		}
	}()
	<-ch
	m.Run(m.ctx)

	m.expect(expectResult{
		state:       AcceptState,
		sequence:    1,
		prepareMsgs: 0,
		commitMsgs:  0,
		outgoing:    0,
	})

	m.Run(context.Background())

	m.expect(expectResult{
		state:       DoneState,
		sequence:    1,
		prepareMsgs: 1,
		commitMsgs:  1,
		outgoing:    3,
	})
}

func TestGossip_Failed(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B"}, "A")
	m.gossipFn = func(msg *MessageReq) error {
		return errors.New("deliberate failure")
	}

	var buf bytes.Buffer
	m.logger.SetOutput(&buf)
	defer func() {
		m.logger.SetOutput(getDefaultLoggerOutput())
	}()
	m.gossip(MessageReq_Preprepare)
	m.logger.Println(buf.String())
	assert.Contains(t, buf.String(), "[ERROR] failed to gossip")
}

func TestGossip_SignProposalFailed(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B"}, "A")
	validator := m.pool.get("A")
	validator.signFn = func(b []byte) ([]byte, error) {
		return nil, errors.New("failed to sign message")
	}

	var buf bytes.Buffer
	m.logger.SetOutput(&buf)
	defer func() {
		m.logger.SetOutput(getDefaultLoggerOutput())
	}()

	m.gossip(MessageReq_Commit)
	m.logger.Println(buf.String())
	assert.Contains(t, buf.String(), "[ERROR] failed to commit seal")
}

type gossipDelegate func(*MessageReq) error

type mockPbft struct {
	*Pbft

	t        *testing.T
	pool     *testerAccountPool
	respMsg  []*MessageReq
	proposal *Proposal
	sequence uint64
	cancelFn context.CancelFunc
	gossipFn gossipDelegate
}

func (m *mockPbft) emitMsg(msg *MessageReq) {
	// convert the address from the address pool
	// from := m.pool.get(string(msg.From)).Address()
	// msg.From = from

	m.Pbft.PushMessage(msg)
}

func (m *mockPbft) addMessage(msg *MessageReq) {
	// convert the address from the address pool
	// from := m.pool.get(string(msg.From)).Address()
	// msg.From = from

	m.state.addMessage(msg)
}

func (m *mockPbft) Gossip(msg *MessageReq) error {
	if m.gossipFn != nil {
		return m.gossipFn(msg)
	}
	m.respMsg = append(m.respMsg, msg)
	return nil
}

func (m *mockPbft) CalculateTimeout() time.Duration {
	return time.Millisecond
}

func newMockPbft(t *testing.T, accounts []string, account string, backendArg ...*mockBackend) *mockPbft {
	pool := newTesterAccountPool()
	pool.add(accounts...)

	m := &mockPbft{
		t:        t,
		pool:     pool,
		respMsg:  []*MessageReq{},
		sequence: 1, // use by default sequence=1
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

	loggerOutput := getDefaultLoggerOutput()

	// initialize pbft
	m.Pbft = New(acct, m,
		WithLogger(log.New(loggerOutput, "", log.LstdFlags)))

	// mock timeout
	m.roundTimeout = func(u uint64) time.Duration { return time.Millisecond }

	// initialize backend mock
	var backend *mockBackend
	if len(backendArg) == 1 && backendArg[0] != nil {
		backend = backendArg[0]
		backend.mock = m
	} else {
		backend = newMockBackend(accounts, m, nil, nil, nil)
	}
	_ = m.Pbft.SetBackend(backend)

	m.state.proposal = &Proposal{
		Data: mockProposal,
		Time: time.Now(),
	}

	ctx, cancelFn := context.WithCancel(context.Background())
	m.Pbft.ctx = ctx
	m.cancelFn = cancelFn

	return m
}

func getDefaultLoggerOutput() io.Writer {
	if os.Getenv("SILENT") == "true" {
		return ioutil.Discard
	}
	return os.Stdout
}

func newMockBackend(
	validatorIds []string,
	mockPbft *mockPbft,
	buildProposal buildProposalDelegate,
	validateProposal validateDelegate,
	isStuck isStuckDelegate) *mockBackend {
	return &mockBackend{
		mock:            mockPbft,
		validators:      newMockValidatorSet(validatorIds).(*valString),
		buildProposalFn: buildProposal,
		validateFn:      validateProposal,
		isStuckFn:       isStuck,
	}
}

func (m *mockPbft) Close() {
	m.cancelFn()
}

func (m *mockPbft) setProposal(p *Proposal) {
	m.proposal = p
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

// expect is a test helper function
// printed information from this one will be skipped
// may be called from simultaneosly from multiple gorutines
func (m *mockPbft) expect(res expectResult) {
	m.t.Helper()

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

type buildProposalDelegate func() (*Proposal, error)
type validateDelegate func([]byte) error
type isStuckDelegate func(uint64) (uint64, bool)
type mockBackend struct {
	mock            *mockPbft
	validators      *valString
	buildProposalFn buildProposalDelegate
	validateFn      validateDelegate
	isStuckFn       isStuckDelegate
}

func (m *mockBackend) Hash(p []byte) []byte {
	h := sha1.New()
	h.Write(p)
	return h.Sum(nil)
}

func (m *mockBackend) BuildProposal() (*Proposal, error) {
	if m.buildProposalFn != nil {
		return m.buildProposalFn()
	}

	if m.mock.proposal == nil {
		panic("add a proposal in the test")
	}
	return m.mock.proposal, nil
}

func (m *mockBackend) Height() uint64 {
	return m.mock.sequence
}

func (m *mockBackend) Validate(proposal []byte) error {
	if m.validateFn != nil {
		return m.validateFn(proposal)
	}
	return nil
}

func (m *mockBackend) IsStuck(num uint64) (uint64, bool) {
	if m.isStuckFn != nil {
		return m.isStuckFn(num)
	}
	return 0, false
}

func (m *mockBackend) Insert(pp *SealedProposal) error {
	// TODO:
	if pp.Proposer == "" {
		return errVerificationFailed
	}
	return nil
}

func (m *mockBackend) ValidatorSet() ValidatorSet {
	return m.validators
}
