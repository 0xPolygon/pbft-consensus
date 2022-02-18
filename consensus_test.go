package pbft

import (
	"bytes"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		Hash: digest,
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

// Test that if build proposal fails, state machine will change state from AcceptState to RoundChangeState.
func TestTransition_AcceptState_Proposer_FailedBuildProposal(t *testing.T) {
	buildProposalFailure := func() (*Proposal, error) {
		return nil, errors.New("failed to build a proposal")
	}

	validatorIds := []string{"A", "B", "C"}
	m := newMockPbft(t, validatorIds, "A", func(backend *mockBackend) {
		backend.HookBuildProposalHandler(buildProposalFailure)
	})
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

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

	m.runCycle(m.ctx)
	assert.True(t, m.IsState(RoundChangeState))
}

// Run state machine from AcceptState, proposer node.
// Artificially induce state machine cancellation and check whether state machine is still in AcceptState.
func TestTransition_AcceptState_Proposer_Cancellation(t *testing.T) {
	testAcceptState_Cancellation(t, true)
}

// Run state machine from AcceptState, non-proposer node.
// Artificially induce state machine cancellation and check whether state machine is still in the AcceptState.
func TestTransition_AcceptState_NonProposer_Cancellation(t *testing.T) {
	testAcceptState_Cancellation(t, false)
}

func testAcceptState_Cancellation(t *testing.T, isProposerNode bool) {
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "D")
	if !isProposerNode {
		m.Pbft.state.proposer = "A"
	}

	m.setState(AcceptState)
	m.setProposal(&Proposal{
		Data: mockProposal,
		Time: time.Now().Add(time.Second),
	})

	go func() {
		m.cancelFn()
	}()

	assert.NotPanics(t, func() { m.runCycle(m.ctx) })
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
	// We are a validator and have a locked state in 'proposal1'.
	// We receive an invalid proposal 'proposal2' with different data.

	i := newMockPbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	// locked proposal
	i.state.proposal = &Proposal{
		Data: mockProposal,
		Hash: digest,
	}
	i.state.lock()

	// emit the wrong locked proposal
	i.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal1,
		Hash:     digest1,
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

	i.state.proposal = &Proposal{
		Data: proposal,
		Hash: digest,
	}
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

// Test that when validating proposal fails, state machine switches to RoundChangeState.
func TestTransition_AcceptState_Validate_ProposalFail(t *testing.T) {
	validateProposalFunc := func(p *Proposal) error {
		return errors.New("failed to validate a proposal")
	}

	validatorIds := []string{"A", "B", "C"}
	m := newMockPbft(t, validatorIds, "C", func(backend *mockBackend) {
		backend.HookValidateHandler(validateProposalFunc)
	})
	m.state.view = ViewMsg(1, 0)
	m.setState(AcceptState)

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

	m.runCycle(m.ctx)

	assert.True(t, m.IsState(RoundChangeState))
}

// Local node sending a messages isn't among validator set, so state machine should set state to SyncState
func TestTransition_AcceptState_NonValidatorNode(t *testing.T) {
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

// Test that when state machine initial state is RoundChange and proposal
func TestTransition_RoundChangeState_Stuck(t *testing.T) {
	isStuckFn := func(num uint64) (uint64, bool) {
		return 0, true
	}

	validatorIds := []string{"A", "B", "C"}
	m := newMockPbft(t, validatorIds, "A", func(backend *mockBackend) {
		backend.HookIsStuckHandler(isStuckFn)
	})
	m.SetState(RoundChangeState)

	m.runCycle(context.Background())
	assert.True(t, m.IsState(SyncState))
}

// Test ValidateState to CommitState transition.
func TestTransition_ValidateState_MoveToCommitState(t *testing.T) {
	// we receive enough prepare messages to lock and commit the proposal
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(ValidateState)

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

// No messages are sent, so ensure that destination state is RoundChangeState and that state machine jumps out of the loop.
func TestTransition_ValidateState_MoveToRoundChangeState(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(ValidateState)

	m.runCycle(context.Background())

	assert.True(t, m.IsState(RoundChangeState))
}

// Send wrong message type within ValidateState and asssure it panics
func TestTransition_ValidateState_WrongMessageType(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.setState(ValidateState)

	// Create preprepare message and push it to validate state message queue
	msg := &MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: mockProposal,
		Hash:     digest,
		View:     ViewMsg(1, 0),
	}
	heap.Push(&m.msgQueue.validateStateQueue, msg)
	assert.PanicsWithError(t, "BUG: Unexpected message type: Preprepare in ValidateState", func() { m.runCycle(context.Background()) })
}

// Test that past and future messages are discarded and state machine transfers from ValidateState to RoundChangeState.
func TestTransition_ValidateState_DiscardMessage(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B"}, "A")
	m.setState(ValidateState)
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

// Test CommitState to DoneState transition.
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

// Test CommitState to RoundChange transition.
func TestTransition_CommitState_RoundChange(t *testing.T) {
	failedInsertion := func(_ *SealedProposal) error {
		return errFailedToInsertProposal
	}

	m := newMockPbft(t, []string{"A", "B", "C"}, "A", func(backend *mockBackend) {
		backend.HookInsertHandler(failedInsertion)
	})
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

// Test exponential timeout for various rounds.
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

// Ensure that DoneState cannot be set as initial state of state machine.
func TestDoneState_RunCycle_Panics(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B", "C"}, "A")
	m.state.view = ViewMsg(1, 0)
	m.SetState(DoneState)

	assert.Panics(t, func() { m.runCycle(context.Background()) })
}

// Test run loop of PBFT state machine.
// Use case #1: Cancellation is triggered and state machine remains in the AcceptState.
// Use case #2: Cancellation is not triggered and state machine converges to the DoneState.
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
	waitSignal := make(chan struct{})
	go func() {
		close(waitSignal)
		for {
			if m.getState() == AcceptState {
				m.cancelFn()
				return
			}
		}
	}()

	<-waitSignal
	// Make sure that if there is a cancellation trigger, state machine remains in the AcceptState.
	m.Run(m.ctx)

	m.expect(expectResult{
		state:       AcceptState,
		sequence:    1,
		prepareMsgs: 0,
		commitMsgs:  0,
		outgoing:    0,
	})

	// Make sure that if there is no cancellation trigger, state machine converges to the DoneState.
	m.Run(context.Background())

	m.expect(expectResult{
		state:       DoneState,
		sequence:    1,
		prepareMsgs: 1,
		commitMsgs:  1,
		outgoing:    3,
	})
}

// One of the validators fails to sign a proposal. Ensure that no messages were added to any message queue.
func TestGossip_SignProposalFailed(t *testing.T) {
	m := newMockPbft(t, []string{"A", "B"}, "A")
	validator := m.pool.get("A")
	validator.signFn = func(b []byte) ([]byte, error) {
		return nil, errors.New("failed to sign message")
	}

	m.gossip(MessageReq_Commit)

	assert.Empty(t, m.msgQueue.acceptStateQueue)
	assert.Empty(t, m.msgQueue.roundChangeStateQueue)
	assert.Empty(t, m.msgQueue.validateStateQueue)
}

// TestPBFT_Persistence aims to validate the persistence problem described in the paper
// for IBFT analysis, Section 5 Persistence analysis, proof for Lemma 9 (https://arxiv.org/pdf/1901.07160.pdf)
//
// Terminology:
// v - random honest validator
// W - set of all nodes (honest and byzantine), without v
// Whonest - set of all honest nodes
// This test assumes numNodes >= 4 && F(numNodes) >= 1
func TestPBFT_Persistence(t *testing.T) {
	nodePrefix := "node_"
	numNodes := uint64(5)

	// Generate block proposals
	var (
		firstProposal  []byte
		secondProposal []byte
		genErr         error
	)

	// The first proposal is for round 0, where Node 0 is the proposer
	if firstProposal, genErr = generateRandomBytes(4); genErr != nil {
		t.Fatalf("unable to generate first proposal, %v", genErr)
	}

	// The second proposal is for round 1, where Node 1 is the proposer
	if secondProposal, genErr = generateRandomBytes(4); genErr != nil {
		t.Fatalf("unable to generate second proposal, %v", genErr)
	}

	getFirstProposal := func() (*Proposal, error) {
		return &Proposal{
			Data: firstProposal,
			Time: time.Now(),
			Hash: hashProposalData(firstProposal),
		}, nil
	}

	getSecondProposal := func() (*Proposal, error) {
		return &Proposal{
			Data: secondProposal,
			Time: time.Now(),
			Hash: hashProposalData(secondProposal),
		}, nil
	}

	// This is invoked at the commit state, where nodes from W should
	// fail to validate a commit signature from Node 4 (byzantine), but Node 2 (v) should
	// not error out, because Node 4 (byzantine) sent him a correctly formed commit message for the first proposal
	invalidateProposal := func(proposal *SealedProposal) error {
		if bytes.Equal(firstProposal, proposal.Proposal.Data) {
			return errors.New("invalid commit signature size")
		}

		return nil
	}

	// Create a cluster of numNodes, including 1 Byzantine node
	cluster := newMockPBFTClusterWithBackends(
		t,
		nodePrefix,
		numNodes,
		map[int]backendConfigCallback{
			// Node 0 is the proposer for the first proposal
			0: func(backend *mockBackend) {
				// Set the hook for building the first proposal for round 0
				backend.HookBuildProposalHandler(getFirstProposal)

				// Since Node 0 is part of Whonest, it should error out due to a
				// malformed commit seal
				backend.HookInsertHandler(invalidateProposal)
			},

			// Node 1 is the proposer for the second block.
			// The assumption is that the second proposal is different from the first one
			1: func(backend *mockBackend) {
				// Set the hook for building the second proposal for round 1
				backend.HookBuildProposalHandler(getSecondProposal)

				// Since Node 1 is part of Whonest, it should error out due to a
				// malformed commit seal
				backend.HookInsertHandler(invalidateProposal)
			},

			3: func(backend *mockBackend) {
				// Since Node 3 is part of Whonest, it should error out due to a
				// malformed commit seal
				backend.HookInsertHandler(invalidateProposal)
			},

			4: func(backend *mockBackend) {
				// Node 4 is the byzantine node, it should follow along other nodes
				// in Whonest into Round Change state after failing to insert a proposal
				backend.HookInsertHandler(invalidateProposal)
			},
		},
	)

	// gossipHandler is a handler for gossip messages that simulates
	// a network environment, where messages are broadcast to all nodes
	gossipHandler := func(msg *MessageReq) error {
		for _, node := range cluster.nodes {
			node.PushMessage(msg)
		}

		return nil
	}

	for _, node := range cluster.nodes {
		node.HookGossipHandler(gossipHandler)
	}

	// v is Node 2
	v := cluster.nodes[2]

	// W is the set of all nodes apart from v (including the byzantine node)
	W := make([]*mockPbft, numNodes-1)
	copy(W[:2], cluster.nodes[:2])
	copy(W[2:], cluster.nodes[3:])

	// Run accept state
	cluster.runAcceptState()

	// Check that all nodes are working with the data after running accept state
	assert.NoError(
		t,
		verifyAllSameProposal(
			cluster.nodes,
			verifyProposalParams{
				proposal: firstProposal,
				proposer: cluster.nodes[0].validator.NodeID(),
			},
		),
	)

	// Everyone is working with the same state [Validate state]
	assert.NoError(t, verifyAllInState(cluster.nodes, ValidateState))

	// Run validate state
	cluster.runValidateState()

	// Everyone is working with the same state [Commit state]
	assert.NoError(t, verifyAllInState(cluster.nodes, CommitState))

	// Save the sequence v is working with
	vSequence := v.state.view.Sequence

	// Run the commit state
	cluster.runCommitState()

	// Nodes in Whonest (Node 0, 1, 3 and 4) should be in the round change state
	// due to receiving a malformed commit signature from the byzantine node 4.
	// We simulate this invalid commit signature by making sure the backend "checked it" and errored out
	assert.NoError(t, verifyAllInState(W, RoundChangeState))

	// Node 2, who received Quorum(n) valid Commit messages should be done with the cycle.
	// When Node 2 is in the Done state, it means it successfully inserted the block
	assert.NoError(t, verifyAllInState([]*mockPbft{v}, DoneState))

	// Run the round change state, so nodes in W can start a new round
	runRoundChangeState(W)

	// Make sure all nodes in W are in round 1
	assert.NoError(t, verifyAllInRound(W, 1))

	// Make sure all nodes in W are in Accept State
	assert.NoError(t, verifyAllInState(W, AcceptState))

	// Run accept state
	runAcceptState(W)

	// Make sure all nodes in W are in Validate State
	assert.NoError(t, verifyAllInState(W, ValidateState))

	// Check that all nodes are working with the data after running accept state
	assert.NoError(
		t,
		verifyAllSameProposal(
			W,
			verifyProposalParams{
				proposal: secondProposal,
				proposer: cluster.nodes[1].validator.NodeID(),
			},
		),
	)

	// Run validate state
	runValidateState(W)

	// Make sure all nodes in W are in Commit State
	assert.NoError(t, verifyAllInState(W, CommitState))

	// Make sure they are all on the same height / sequence (optional check)
	assert.NoError(t, verifyAllAtSequence(W, 1))

	// Save the sequence W is working on
	wSequence := W[0].state.view.Sequence

	// Run commit state
	runCommitState(W)

	// Make sure all nodes in W are in Done State
	assert.NoError(t, verifyAllInState(W, DoneState))

	// At this point, all nodes in W have inserted the second proposal, for the same sequence as v.
	// v has inserted the first proposal, which means they have differing data for the same sequence
	assert.Equal(t, vSequence, wSequence)
}
