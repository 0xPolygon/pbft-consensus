package pbft

import (
	"fmt"
	mrand "math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func init() {
	mrand.Seed(time.Now().UnixNano())
}

func TestState_FaultyNodesCount(t *testing.T) {
	cases := []struct {
		TotalNodesCount, FaultyNodesCount int
	}{
		{0, 0},
		{1, 0},
		{2, 0},
		{3, 0},
		{4, 1},
		{5, 1},
		{6, 1},
		{7, 2},
		{8, 2},
		{9, 2},
		{10, 3},
		{99, 32},
		{100, 33},
	}
	for _, c := range cases {
		s := newState()
		s.validators = convertToMockValidatorSet(generateValidatorNodes(c.TotalNodesCount, "validator"))

		assert.Equal(t, c.FaultyNodesCount, s.MaxFaultyNodes())
	}
}

func Test_QuorumSize(t *testing.T) {
	cases := []struct {
		TotalNodesCount, QuorumSize int
	}{
		{1, 1},
		{2, 1},
		{3, 1},
		{4, 3},
		{5, 3},
		{6, 3},
		{7, 5},
		{8, 5},
		{9, 5},
		{10, 7},
		{100, 67},
	}

	for _, c := range cases {
		assert.Equal(t, c.QuorumSize, QuorumSize(c.TotalNodesCount))
	}
}

func TestState_ValidNodesCount(t *testing.T) {
	cases := []struct {
		TotalNodesCount, ValidNodesCount int
	}{
		{1, 0},
		{2, 0},
		{3, 0},
		{4, 2},
		{5, 2},
		{6, 2},
		{7, 4},
		{8, 4},
		{9, 4},
		{10, 6},
		{100, 66},
	}
	for _, c := range cases {
		s := newState()
		s.validators = convertToMockValidatorSet(generateValidatorNodes(c.TotalNodesCount, "validator"))

		assert.Equal(t, c.ValidNodesCount, s.NumValid())
	}
}

func TestState_AddMessages(t *testing.T) {
	pool := newTesterAccountPool()
	validatorIds := []string{"A", "B", "C", "D"}
	pool.add(validatorIds...)

	s := newState()
	s.validators = pool.validatorSet()

	// Send message from node which is not amongst validator nodes
	s.addMessage(createMessage("E", MessageReq_Prepare))
	assert.Empty(t, s.committed)
	assert.Empty(t, s.prepared)
	assert.Empty(t, s.roundMessages)

	// -- test committed messages --
	s.addMessage(pool.createMessage("A", MessageReq_Commit))
	s.addMessage(pool.createMessage("B", MessageReq_Commit))
	s.addMessage(pool.createMessage("B", MessageReq_Commit))

	assert.Equal(t, 2, s.numCommitted())

	// -- test prepare messages --
	s.addMessage(pool.createMessage("C", MessageReq_Prepare))
	s.addMessage(pool.createMessage("C", MessageReq_Prepare))
	s.addMessage(pool.createMessage("D", MessageReq_Prepare))

	assert.Equal(t, 2, s.numPrepared())

	// -- test round change messages --
	rounds := 2
	for round := 0; round < rounds; round++ {
		for i := 0; i < s.validators.Len(); i++ {
			s.addMessage(pool.createMessage(validatorIds[i], MessageReq_RoundChange, uint64(round)))
		}
	}

	for round := 0; round < rounds; round++ {
		assert.Equal(t, rounds, len(s.roundMessages))
		msgsPerRound := s.roundMessages[uint64(round)]
		assert.Equal(t, s.validators.Len(), len(msgsPerRound))
	}
}

func TestState_MaxRound_Found(t *testing.T) {
	validatorsCount := 5
	roundsCount := 6
	s := newState()

	validatorIds := make([]string, validatorsCount)
	for i := 0; i < validatorsCount; i++ {
		validatorId := fmt.Sprintf("validator_%d", i)
		validatorIds[i] = validatorId
	}
	s.validators = newMockValidatorSet(validatorIds)

	for round := 0; round < roundsCount; round++ {
		if round%2 == 1 {
			for _, validatorId := range validatorIds {
				s.addMessage(createMessage(validatorId, MessageReq_RoundChange, uint64(round)))
			}
		} else {
			s.addMessage(createMessage(validatorIds[mrand.Intn(validatorsCount)], MessageReq_RoundChange, uint64(round)))
		}
	}

	maxRound, found := s.maxRound()
	assert.Equal(t, uint64(5), maxRound)
	assert.Equal(t, true, found)
}

func TestState_MaxRound_NotFound(t *testing.T) {
	validatorsCount := 7
	s := newState()

	validatorIds := make([]string, validatorsCount)
	for i := 0; i < validatorsCount; i++ {
		validatorIds[i] = fmt.Sprintf("validator_%d", i)
	}
	s.validators = newMockValidatorSet(validatorIds)

	// Send wrong message type from some validator, whereas roundMessages map is empty
	s.addMessage(createMessage(validatorIds[0], MessageReq_Preprepare))

	maxRound, found := s.maxRound()
	assert.Equal(t, maxRound, uint64(0))
	assert.Equal(t, found, false)

	// Seed insufficient "RoundChange" messages count, so that maxRound isn't going to be found
	for round := range validatorIds {
		if round%2 == 0 {
			// Each even round should populate more than one "RoundChange" messages, but just enough that we don't reach census (max faulty nodes+1)
			for i := 0; i < s.MaxFaultyNodes(); i++ {
				s.addMessage(createMessage(validatorIds[mrand.Intn(validatorsCount)], MessageReq_RoundChange, uint64(round)))
			}
		} else {
			s.addMessage(createMessage(validatorIds[mrand.Intn(validatorsCount)], MessageReq_RoundChange, uint64(round)))
		}
	}

	maxRound, found = s.maxRound()
	assert.Equal(t, uint64(0), maxRound)
	assert.Equal(t, false, found)
}

func TestState_AddRoundMessage(t *testing.T) {
	s := newState()
	s.validators = newMockValidatorSet([]string{"A", "B"})

	roundMessageSize := s.AddRoundMessage(createMessage("A", MessageReq_Commit, 0))
	assert.Equal(t, 0, roundMessageSize)
	assert.Equal(t, 0, len(s.roundMessages))

	s.AddRoundMessage(createMessage("A", MessageReq_RoundChange, 0))
	s.AddRoundMessage(createMessage("A", MessageReq_RoundChange, 1))
	s.AddRoundMessage(createMessage("A", MessageReq_RoundChange, 2))

	roundMessageSize = s.AddRoundMessage(createMessage("B", MessageReq_RoundChange, 2))
	assert.Equal(t, 2, roundMessageSize)

	s.AddRoundMessage(createMessage("B", MessageReq_RoundChange, 3))
	assert.Equal(t, 4, len(s.roundMessages))

	assert.Empty(t, s.prepared)
	assert.Empty(t, s.committed)
}

func TestState_addPrepared(t *testing.T) {
	s := newState()
	validatorIds := []string{"A", "B"}
	s.validators = newMockValidatorSet(validatorIds)

	s.addPrepared(createMessage("A", MessageReq_Commit))
	assert.Equal(t, 0, len(s.prepared))

	s.addPrepared(createMessage("A", MessageReq_Prepare))
	s.addPrepared(createMessage("B", MessageReq_Prepare))

	assert.Equal(t, len(validatorIds), len(s.prepared))
	assert.Empty(t, s.committed)
	assert.Empty(t, s.roundMessages)
}

func TestState_addCommitted(t *testing.T) {
	s := newState()
	validatorIds := []string{"A", "B"}
	s.validators = newMockValidatorSet(validatorIds)

	s.addCommitted(createMessage("A", MessageReq_Prepare))
	assert.Empty(t, 0, s.committed)

	s.addCommitted(createMessage("A", MessageReq_Commit))
	s.addCommitted(createMessage("B", MessageReq_Commit))

	assert.Equal(t, len(validatorIds), len(s.committed))
	assert.Empty(t, s.prepared)
	assert.Empty(t, s.roundMessages)
}

func TestState_Copy(t *testing.T) {
	originalMsg := createMessage("A", MessageReq_Preprepare, 0)
	copyMsg := originalMsg.Copy()
	assert.NotSame(t, originalMsg, copyMsg)
	assert.Equal(t, originalMsg, copyMsg)
}

func TestState_Lock_Unlock(t *testing.T) {
	s := newState()
	proposalData := make([]byte, 2)
	mrand.Read(proposalData)
	s.proposal = &Proposal{
		Data: proposalData,
		Time: time.Now(),
	}
	s.lock()
	assert.True(t, s.locked)
	assert.NotNil(t, s.proposal)

	s.unlock()
	assert.False(t, s.locked)
	assert.Nil(t, s.proposal)
}

func TestState_GetSequence(t *testing.T) {
	s := newState()
	s.view = &View{Sequence: 3, Round: 0}
	assert.True(t, s.GetSequence() == 3)
}

func TestState_getCommittedSeals(t *testing.T) {
	pool := newTesterAccountPool()
	pool.add("A", "B", "C", "D")

	s := newState()
	s.validators = pool.validatorSet()

	s.addCommitted(createMessage("A", MessageReq_Commit))
	s.addCommitted(createMessage("B", MessageReq_Commit))
	committedSeals := s.getCommittedSeals()

	assert.Len(t, committedSeals, 2)
	assert.True(t, &committedSeals[0] != &s.committed["A"].Seal)
	foundSeal := false
	for _, seal := range committedSeals {
		if reflect.DeepEqual(seal, s.committed["A"].Seal) {
			foundSeal = true
		}
	}
	assert.True(t, &committedSeals[0] != &s.committed["A"].Seal)
	assert.True(t, foundSeal)

	foundSeal = false
	for _, seal := range committedSeals {
		if reflect.DeepEqual(seal, s.committed["B"].Seal) {
			foundSeal = true
		}
	}

	assert.True(t, &committedSeals[1] != &s.committed["B"].Seal)
	assert.True(t, foundSeal)
}

func TestMsgType_ToString(t *testing.T) {
	expectedMapping := map[MsgType]string{
		MessageReq_RoundChange: "RoundChange",
		MessageReq_Preprepare:  "Preprepare",
		MessageReq_Commit:      "Commit",
		MessageReq_Prepare:     "Prepare",
	}

	for msgType, expected := range expectedMapping {
		assert.Equal(t, expected, msgType.String())
	}
}

func TestPbftState_ToString(t *testing.T) {
	expectedMapping := map[PbftState]string{
		AcceptState:      "AcceptState",
		RoundChangeState: "RoundChangeState",
		ValidateState:    "ValidateState",
		CommitState:      "CommitState",
		SyncState:        "SyncState",
		DoneState:        "DoneState",
	}

	for pbftState, expected := range expectedMapping {
		assert.Equal(t, expected, pbftState.String())
	}
}
