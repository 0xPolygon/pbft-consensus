package pbft

import (
	"container/heap"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	mockProposal = []byte{0x1, 0x2, 0x3}
	digest       = []byte{0x1}

	mockProposal1 = []byte{0x1, 0x2, 0x3, 0x4}
	digest1       = []byte{0x2}
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

	i.state.lock()
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
	backend := newMockBackend(validatorIds, nil).HookBuildProposalHandler(buildProposalFailure)

	m := newMockPbft(t, validatorIds, "A", backend)
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
	i.state.lock()

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
	backend := newMockBackend(validatorIds, nil).HookValidateHandler(validateProposalFunc)

	m := newMockPbft(t, validatorIds, "C", backend)
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

	m.setState(RoundChangeState)

	// Jump out from a state machine loop straight away after round 2
	waitSignal := make(chan struct{})
	go func() {
		close(waitSignal)
		for {
			if m.state.GetCurrentRound() == 2 {
				m.cancelFn()
				return
			}
		}
	}()

	<-waitSignal

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
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
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
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
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
	m := newMockPbft(t, []string{"A", "B", "C", "D"}, "A")
	m.Close()

	m.addMessage(&MessageReq{
		From: "B",
		Type: MessageReq_RoundChange,
		View: &View{
			Round:    10,
			Sequence: 1,
		},
	})
	m.addMessage(&MessageReq{
		From: "C",
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
	mockBackend := newMockBackend(validatorIds, nil).HookIsStuckHandler(isStuckFn)

	m := newMockPbft(t, validatorIds, "A", mockBackend)
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
			timeout := exponentialTimeoutDuration(tc.round)
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

func TestRoundChange_PropertyMajorityOfVotingPowerAggreement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numberOfNodes := rapid.IntRange(4, 100).Draw(t, "Generate number of nodes").(int)
		stake := rapid.SliceOfN(rapid.Uint64Range(1, 1000000), numberOfNodes, numberOfNodes).Draw(t, "Generate stake").([]uint64)
		randomValidator := rapid.IntRange(0, 99).Draw(t, "Get random validator").(int)
		validators := make([]NodeID, numberOfNodes)
		votingPower := make(map[NodeID]uint64)

		for i := range validators {
			nodeId := NodeID(strconv.Itoa(i))
			validators[i] = nodeId
			votingPower[nodeId] = stake[i]
		}
		validatorSet := NewValStringStub(validators, votingPower)

		maxNodeID := numberOfNodes - 1

		node := New(ValidatorKeyMock(strconv.Itoa(randomValidator)), &TransportStub{
			GossipFunc: func(ft *TransportStub, msg *MessageReq) error {
				return nil
			},
		}, WithLogger(log.New(io.Discard, "", log.LstdFlags)))
		node.state.validators = validatorSet
		node.state.view = &View{
			1,
			1,
		}
		err := node.state.initializeVotingInfo()
		require.NoError(t, err)

		votes := rapid.SliceOfDistinct(rapid.IntRange(0, maxNodeID), func(v int) int {
			return v
		}).Filter(func(votes []int) bool {
			var votesVP uint64
			for i := range votes {
				votesVP += stake[votes[i]]
			}
			return votesVP >= node.state.QuorumSize()
		}).Draw(t, "Select arbitrary nodes that have majority of voting power").([]int)

		for _, voterID := range votes {
			node.PushMessage(&MessageReq{Type: MessageReq_RoundChange, From: NodeID(strconv.Itoa(voterID)), View: ViewMsg(1, 2)})
		}

		//for sending rounchange message
		node.state.err = errors.New("skip")
		node.state.setState(RoundChangeState)

		node.ctx = context.Background()
		node.runRoundChangeState(context.Background())

		if node.getState() != AcceptState {
			t.Error("Invalid state")
		}
	})
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
	if msg.Hash == nil {
		// Use default safe value
		msg.Hash = digest
	}
	m.Pbft.PushMessage(msg)
}

func (m *mockPbft) addMessage(msg *MessageReq) {
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
		WithLogger(log.New(loggerOutput, "", log.LstdFlags)),
		WithRoundTimeout(func(round uint64) <-chan time.Time {
			return time.NewTimer(time.Millisecond).C
		}))

	// initialize backend mock
	var backend *mockBackend
	if len(backendArg) == 1 && backendArg[0] != nil {
		backend = backendArg[0]
		backend.mock = m
	} else {
		backend = newMockBackend(accounts, m)
	}
	_ = m.Pbft.SetBackend(backend)

	m.state.proposal = &Proposal{
		Data: mockProposal,
		Time: time.Now(),
		Hash: digest,
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

func newMockBackend(validatorIds []string, mockPbft *mockPbft) *mockBackend {
	return &mockBackend{
		mock:       mockPbft,
		validators: newMockValidatorSet(validatorIds).(*ValStringStub),
	}
}

func (m *mockPbft) Close() {
	m.cancelFn()
}

// setProposal sets the proposal that will get returned if the pbft consensus
// calls the backend 'BuildProposal' function.
func (m *mockPbft) setProposal(p *Proposal) {
	if p.Hash == nil {
		h := sha1.New()
		h.Write(p.Data)
		p.Hash = h.Sum(nil)
	}
	m.proposal = p
}

type expectResult struct {
	state    State
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
// may be called from simultaneously from multiple gorutines
func (m *mockPbft) expect(res expectResult) {
	m.t.Helper()

	if sequence := m.state.view.Sequence; sequence != res.sequence {
		m.t.Fatalf("incorrect sequence %d %d", sequence, res.sequence)
	}
	if round := m.state.GetCurrentRound(); round != res.round {
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
	if m.state.IsLocked() != res.locked {
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
type validateDelegate func(*Proposal) error
type isStuckDelegate func(uint64) (uint64, bool)

type mockBackend struct {
	mock            *mockPbft
	validators      *ValStringStub
	buildProposalFn buildProposalDelegate
	validateFn      validateDelegate
	isStuckFn       isStuckDelegate
}

func (m *mockBackend) HookBuildProposalHandler(buildProposal buildProposalDelegate) *mockBackend {
	m.buildProposalFn = buildProposal
	return m
}

func (m *mockBackend) HookValidateHandler(validate validateDelegate) *mockBackend {
	m.validateFn = validate
	return m
}

func (m *mockBackend) HookIsStuckHandler(isStuck isStuckDelegate) *mockBackend {
	m.isStuckFn = isStuck
	return m
}

func (m *mockBackend) ValidateCommit(from NodeID, seal []byte) error {
	return nil
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

func (m *mockBackend) Validate(proposal *Proposal) error {
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

func (m *mockBackend) Init(*RoundInfo) {
}
