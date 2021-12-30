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
	"go.opentelemetry.io/otel/trace"
)

var (
	mockProposal  = []byte{0x1, 0x2, 0x3}
	mockProposal1 = []byte{0x1, 0x2, 0x3, 0x4}
)

func Test_InitializeConfigOptionsSet(t *testing.T) {
	t.Parallel()

	pool := newTesterAccountPool()
	pool.add("A", "B", "C")

	m := &mockPbft{
		t:        t,
		pool:     pool,
		respMsg:  []*MessageReq{},
		sequence: 1, // use by default sequence=1
	}

	timeout := 1 * time.Millisecond
	proposalTimeout := 2 * time.Millisecond
	mockTracer := &mockTracer{}
	mockTimeoutProvider := &mockTimeoutProvider{}

	assert.NotPanics(t, func() {
		m.Pbft = New(pool.get("A"), m,
			WithTimeout(timeout),
			WithProposalTimeout(proposalTimeout),
			WithTracer(mockTracer),
			WithTimeoutProvider(mockTimeoutProvider))
	}, "New function failed to execute")
	assert.Same(t, mockTracer, m.Pbft.config.Tracer)
	assert.Same(t, mockTimeoutProvider, m.Pbft.config.TimeoutProvider)

	assert.Equal(t, timeout, m.Pbft.config.Timeout)
	assert.Equal(t, proposalTimeout, m.Pbft.config.ProposalTimeout)
	assert.Equal(t, mockTracer, m.Pbft.config.Tracer)
	assert.Equal(t, mockTimeoutProvider, m.Pbft.config.TimeoutProvider)
}

func TestTransition_AcceptState(t *testing.T) {
	t.Parallel()

	for sc, testFn := range map[string]func(t *testing.T){
		"move to sync state":             acceptState_SyncStateTransition,
		"proposer create proposal":       acceptState_ProposerPropose,
		"proposer locked":                acceptState_ProposerLocked,
		"proposer failed build proposal": acceptState_ProposerFailedBuildProposal,
		"proposer cancellation":          acceptState_ProposerCancellation,
		"non proposer cancellation":      acceptState_NonProposerCancellation,
		"validator verify as correct":    acceptState_ValidatorVerifyCorrect,
		"validator proposer invalid":     acceptState_ValidatorProposerInvalid,
		"validator locked wrong state":   acceptState_ValidatorLockWrong,
		"validator locked correct state": acceptState_ValidatorLockCorrect,
		"validate proposal failure":      acceptState_ValidateProposalFail,
		"run from non validator node":    acceptState_NonValidatorNode,
	} {
		scenario := sc
		testCase := testFn
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			testCase(t)
		})
	}
}

func TestTransition_RoundChangeState(t *testing.T) {
	t.Parallel()

	for sc, testFn := range map[string]func(t *testing.T){
		"round catchup":                        roundChangeState_CatchupRound,
		"timeout":                              roundChangeState_Timeout,
		"weak certificate":                     roundChangeState_WeakCertificate,
		"start new round due to error":         roundChangeState_ErrStartNewRound,
		"start new round due to timeout":       roundChangeState_StartNewRound,
		"catchup highest round due to timeout": roundChangeState_MaxRound,
		"round change stuck":                   roundChangeState_Stuck,
	} {
		scenario := sc
		testCase := testFn
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			testCase(t)
		})
	}
}

func TestTransition_ValidateState(t *testing.T) {
	t.Parallel()

	for sc, testFn := range map[string]func(t *testing.T){
		"validate to commit state":       validateState_MoveToCommitState,
		"validate to round change state": validateState_MoveToRoundChangeState,
		"validate wrong message type":    validateState_WrongMessageType,
		"discarded message":              validateState_DiscardMessage,
	} {
		scenario := sc
		testCase := testFn
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			testCase(t)
		})
	}
}

func validateState_MoveToCommitState(t *testing.T) {
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

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence:    1,
		state:       CommitState,
		prepareMsgs: 3,
		commitMsgs:  3, // Commit messages (A proposer sent commit via state machine loop, C and D sent commit via emit message)
		locked:      true,
		outgoing:    1, // A commit message
	})
}

func validateState_MoveToRoundChangeState(t *testing.T) {
	// No messages are sent, so we are changing state to round change state and jumping out of the state machine loop
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(ValidateState)

	m.runCycle(m.ctx)

	assert.True(t, m.IsState(RoundChangeState))
}

func validateState_WrongMessageType(t *testing.T) {
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
	assert.PanicsWithError(t, "BUG: Unexpected message type: Preprepare in ValidateState", func() { m.runCycle(m.ctx) })
}

func validateState_DiscardMessage(t *testing.T) {
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

	m.runCycle(m.ctx)
	m.expect(expectResult{
		state:       RoundChangeState,
		round:       2,
		sequence:    1,
		prepareMsgs: 0,
		commitMsgs:  0,
		outgoing:    0})
}

func TestTransition_AcceptState_Validator_VerifyFails(t *testing.T) {
	t.Skip("involves validation of hash that is not done yet")

	m := newMockPbft(t, []string{"A", "B", "C"}, "B")
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

	// A sends the message
	m.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal,
		View:     ViewMsg(1, 0),
	})

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
		err:      errVerificationFailed,
	})
}

func TestExponentialTimeout(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		description string
		exponent    uint64
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

	for _, testCase := range testCases {
		tc := testCase // rebind tc into this lexical scope
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			defaultTimeoutProvider := &DefaultTimeoutProvider{}
			timeout := defaultTimeoutProvider.CalculateTimeout(tc.exponent)
			require.Equal(t, tc.expected, timeout, fmt.Sprintf("timeout should be %s", tc.expected))
		})
	}
}

func TestTransition_CommitState(t *testing.T) {
	for sc, testFn := range map[string]func(t *testing.T){
		"move to done state":         commitState_DoneState,
		"move to round change state": commitState_RoundChange,
	} {
		scenario := sc
		testCase := testFn
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			testCase(t)
		})
	}

}

func commitState_DoneState(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.state.view = ViewMsg(1, 0)
	m.state.proposer = "A"
	m.setState(CommitState)

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    DoneState,
	})
}

func commitState_RoundChange(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.state.view = ViewMsg(1, 0)
	m.setState(CommitState)

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
		err:      errFailedToInsertProposal,
	})
	assert.True(t, m.IsState(RoundChangeState))
}

func TestDoneState_RunCycle_Panics(t *testing.T) {
	t.Parallel()

	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.state.view = ViewMsg(1, 0)
	m.SetState(DoneState)

	assert.Panics(t, func() { m.runCycle(m.ctx) })
}

func TestPbft_Run(t *testing.T) {
	t.Parallel()

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

func acceptState_SyncStateTransition(t *testing.T) {
	// we are in AcceptState and we are not in the validators list
	// means that we have been removed as validator, move to sync state
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "")
	m.setState(AcceptState)

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    SyncState,
	})
}

func acceptState_ProposerPropose(t *testing.T) {
	// we are in AcceptState and we are the proposer, it needs to:
	// 1. create a proposal
	// 2. wait for the delay
	// 3. send a preprepare message
	// 4. send a prepare message
	// 5. move to ValidateState

	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(AcceptState)

	m.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now().Add(1 * time.Second),
	})

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		outgoing: 2, // preprepare and prepare
		state:    ValidateState,
	})
}

func acceptState_ProposerLocked(t *testing.T) {
	// we are in AcceptState, we are the proposer but the value is locked.
	// it needs to send the locked proposal again
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(AcceptState)

	m.state.locked = true
	m.state.proposal = &Proposal{
		Data: mockProposal,
	}

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    ValidateState,
		locked:   true,
		outgoing: 2, // preprepare and prepare
	})
	assert.Equal(t, m.state.proposal.Data, mockProposal)
}

func acceptState_ValidatorVerifyCorrect(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "B")
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

	// A sends the message
	m.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal,
		View:     ViewMsg(1, 0),
	})

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    ValidateState,
		outgoing: 1, // prepare
	})
}

func acceptState_ValidatorProposerInvalid(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "B")
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

	// A is the proposer but C sends the propose, we do not fail
	// but wait for timeout to move to roundChange state
	m.emitMsg(&MessageReq{
		From:     "C",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal,
		View:     ViewMsg(1, 0),
	})
	m.forceTimeout()

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
	})
}

func acceptState_ProposerCancellation(t *testing.T) {
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

func acceptState_NonProposerCancellation(t *testing.T) {
	// We are in the AcceptState, we are non proposer node.
	// Cancellation occurs, so we are checking whether we are still in AcceptState.
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "D")
	m.Pbft.state.proposer = "A"
	m.timeoutProvider = &mockTimeoutProvider{timeout: time.Second}
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

func acceptState_ValidatorLockWrong(t *testing.T) {
	// we are a validator and have a locked state in 'proposal1'.
	// We receive an invalid proposal 'proposal2' with different data.

	m := newMockPbft(t, []string{"A", "B", "C"}, "B")
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

	// locked proposal
	m.state.proposal = &Proposal{
		Data: mockProposal,
	}
	m.state.lock()

	// emit the wrong locked proposal
	m.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal1,
		View:     ViewMsg(1, 0),
	})

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
		locked:   true,
		err:      errIncorrectLockedProposal,
	})
}

func acceptState_ValidatorLockCorrect(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "B")
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

	// locked proposal
	proposal := mockProposal

	m.state.proposal = &Proposal{Data: proposal}
	m.state.locked = true

	m.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: proposal,
		View:     ViewMsg(1, 0),
	})

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		state:    ValidateState,
		locked:   true,
		outgoing: 1, // prepare message
	})
}

func acceptState_ProposerFailedBuildProposal(t *testing.T) {
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

func acceptState_ValidateProposalFail(t *testing.T) {
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

func acceptState_NonValidatorNode(t *testing.T) {
	// Node sending a messages isn't among validator set, so state machine should set state to SyncState
	m := newMockPbft(t, []string{"A", "B", "C"}, "")
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)
	m.runCycle(m.ctx)

	m.expect(expectResult{
		state:    SyncState,
		sequence: 1,
	})
}

func roundChangeState_CatchupRound(t *testing.T) {
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
	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		round:    2,
		outgoing: 1, // our new round change
		state:    AcceptState,
	})
}

func roundChangeState_Timeout(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")

	m.forceTimeout()
	m.setState(RoundChangeState)
	m.Close()

	// increases to round 1 at the beginning of the round and sends
	// one RoundChange message.
	// After the timeout, it increases to round 2 and sends another
	// / RoundChange message.
	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		round:    2,
		outgoing: 2, // two round change messages
		state:    RoundChangeState,
	})
}

func roundChangeState_WeakCertificate(t *testing.T) {
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

	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		round:    2,
		outgoing: 2, // two round change messages (0->1, 1->2 after weak certificate)
		state:    RoundChangeState,
	})
}

func roundChangeState_ErrStartNewRound(t *testing.T) {
	// if we start a round change because there was an error we start
	// a new round right away
	m := newMockPbft(t, []string{"A", "B"}, "A")
	m.Close()

	m.state.err = errVerificationFailed

	m.setState(RoundChangeState)
	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		round:    1,
		state:    RoundChangeState,
		outgoing: 1,
	})
}

func roundChangeState_StartNewRound(t *testing.T) {
	// if we start round change due to a state timeout and we are on the
	// correct sequence, we start a new round
	m := newMockPbft(t, []string{"A", "B"}, "A")
	m.Close()

	m.setState(RoundChangeState)
	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		round:    1,
		state:    RoundChangeState,
		outgoing: 1,
	})
}

func roundChangeState_MaxRound(t *testing.T) {
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
	m.runCycle(m.ctx)

	m.expect(expectResult{
		sequence: 1,
		round:    10,
		state:    RoundChangeState,
		outgoing: 1,
	})
}

func roundChangeState_Stuck(t *testing.T) {
	isStuckFn := func(num uint64) (uint64, bool) {
		return 0, true
	}

	validatorIds := []string{"A", "B", "C"}
	mockBackend := newMockBackend(validatorIds, nil, nil, nil, isStuckFn)

	m := newMockPbft(t, validatorIds, "A", mockBackend)
	m.SetState(RoundChangeState)

	m.runCycle(m.ctx)
	assert.True(t, m.IsState(SyncState))
}

func TestGossip(t *testing.T) {
	for sc, testFn := range map[string]func(t *testing.T){
		"transport gossip failed":     transportGossip_Failed,
		"gossip sign proposal failed": gossip_SignProposalFailed,
	} {
		scenario := sc
		testCase := testFn
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			testCase(t)
		})
	}
}

func transportGossip_Failed(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B"}, "A")
	m.gossipFn = func(msg *MessageReq) error {
		return errors.New("deliberate failure")
	}

	var buf bytes.Buffer
	m.logger.SetOutput(&buf)
	defer func() {
		m.logger.SetOutput(getDefaultLoggerOutput())
	}()
	_ = m.gossip(MessageReq_Preprepare)
	m.logger.Println(buf.String())
	assert.Contains(t, buf.String(), "[ERROR] failed to gossip")
}

func gossip_SignProposalFailed(t *testing.T) {
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

	err := m.gossip(MessageReq_Commit)
	m.logger.Println(buf.String())
	assert.Equal(t, err.Error(), "failed to sign message")
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
	m.Pbft = New(acct, m, WithLogger(log.New(loggerOutput, "", log.LstdFlags)),
		WithTimeoutProvider(&mockTimeoutProvider{}))

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

type mockTracer struct {
}

func (t *mockTracer) Start(_ context.Context, _ string, _ ...trace.SpanStartOption) (context.Context, trace.Span) {
	return context.Background(), nil
}

type mockTimeoutProvider struct {
	timeout time.Duration
}

func (p *mockTimeoutProvider) CalculateTimeout(_ uint64) time.Duration {
	if p.timeout > 0 {
		return p.timeout
	}
	return time.Millisecond
}
