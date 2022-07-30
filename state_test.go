package pbft

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"fmt"
	mrand "math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	mrand.Seed(time.Now().UnixNano())
}

// Helper function which enables creation of MessageReq.
func createMessage(sender string, messageType MsgType, round ...uint64) *MessageReq {
	seal := make([]byte, 2)
	mrand.Read(seal)

	r := uint64(0)
	if len(round) > 0 {
		r = round[0]
	}

	msg := &MessageReq{
		From:     NodeID(sender),
		Type:     messageType,
		View:     &View{Round: r},
		Seal:     seal,
		Proposal: mockProposal,
	}
	return msg
}

func TestState_AddMessages(t *testing.T) {
	pool := newTesterAccountPool()
	validatorIds := []string{"A", "B", "C", "D"}
	pool.add(validatorIds...)

	s, err := initState(pool)
	require.NoError(t, err)
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

	validatorIds := make([]string, validatorsCount)
	for i := 0; i < validatorsCount; i++ {
		validatorId := fmt.Sprintf("validator_%d", i)
		validatorIds[i] = validatorId
	}
	pool := newTesterAccountPool()
	pool.add(validatorIds...)
	s, err := initState(pool)
	require.NoError(t, err)

	for round := 0; round < roundsCount; round++ {
		if round%2 == 1 {
			for _, validatorId := range validatorIds {
				s.addMessage(pool.createMessage(validatorId, MessageReq_RoundChange, uint64(round)))
			}
		} else {
			s.addMessage(pool.createMessage(validatorIds[mrand.Intn(validatorsCount)], MessageReq_RoundChange, uint64(round)))
		}
	}

	maxRound, found := s.maxRound()
	assert.Equal(t, uint64(5), maxRound)
	assert.Equal(t, true, found)
}

func TestState_MaxRound_NotFound(t *testing.T) {
	validatorsCount := 7

	validatorIds := make([]string, validatorsCount)
	for i := 0; i < validatorsCount; i++ {
		validatorIds[i] = fmt.Sprintf("validator_%d", i)
	}
	pool := newTesterAccountPool()
	pool.add(validatorIds...)
	s, err := initState(pool)
	require.NoError(t, err)

	// Send wrong message type from some validator, whereas roundMessages map is empty
	s.addMessage(createMessage(validatorIds[0], MessageReq_Preprepare))

	maxRound, found := s.maxRound()
	assert.Equal(t, maxRound, uint64(0))
	assert.Equal(t, found, false)

	// Seed insufficient "RoundChange" messages count, so that maxRound isn't going to be found
	for round := range validatorIds {
		if round%2 == 0 {
			// Each even round should populate more than one "RoundChange" messages, but just enough that we don't reach census (max faulty nodes+1)
			for i := 0; i < int(s.MaxFaultyVotingPower()); i++ {
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

	roundMessageSize := s.addRoundChangeMsg(createMessage("A", MessageReq_Commit, 0))
	assert.Equal(t, 0, roundMessageSize)
	assert.Equal(t, 0, len(s.roundMessages))

	s.addRoundChangeMsg(createMessage("A", MessageReq_RoundChange, 0))
	s.addRoundChangeMsg(createMessage("A", MessageReq_RoundChange, 1))
	s.addRoundChangeMsg(createMessage("A", MessageReq_RoundChange, 2))

	roundMessageSize = s.addRoundChangeMsg(createMessage("B", MessageReq_RoundChange, 2))
	assert.Equal(t, 2, roundMessageSize)

	s.addRoundChangeMsg(createMessage("B", MessageReq_RoundChange, 3))
	assert.Equal(t, 4, len(s.roundMessages))

	assert.Empty(t, s.prepared)
	assert.Empty(t, s.committed)
}

func TestState_addPrepared(t *testing.T) {
	s := newState()
	validatorIds := []string{"A", "B"}
	s.validators = newMockValidatorSet(validatorIds)

	s.addPrepareMsg(createMessage("A", MessageReq_Commit))
	assert.Equal(t, 0, len(s.prepared))

	s.addPrepareMsg(createMessage("A", MessageReq_Prepare))
	s.addPrepareMsg(createMessage("B", MessageReq_Prepare))

	assert.Equal(t, len(validatorIds), len(s.prepared))
	assert.Empty(t, s.committed)
	assert.Empty(t, s.roundMessages)
}

func TestState_addCommitted(t *testing.T) {
	s := newState()
	validatorIds := []string{"A", "B"}
	s.validators = newMockValidatorSet(validatorIds)

	s.addCommitMsg(createMessage("A", MessageReq_Prepare))
	assert.Empty(t, 0, s.committed)

	s.addCommitMsg(createMessage("A", MessageReq_Commit))
	s.addCommitMsg(createMessage("B", MessageReq_Commit))

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
	assert.True(t, s.IsLocked())
	assert.NotNil(t, s.proposal)

	s.unlock()
	assert.False(t, s.IsLocked())
	assert.Nil(t, s.proposal)
}

func TestState_GetSequence(t *testing.T) {
	s := newState()
	s.view = &View{Sequence: 3, Round: 0}
	assert.True(t, s.GetSequence() == 3)
}

func TestState_getCommittedSeals(t *testing.T) {
	pool := newTesterAccountPool()
	pool.add("A", "B", "C", "D", "E")

	s := newState()
	s.validators = pool.validatorSet()

	s.addCommitMsg(createMessage("A", MessageReq_Commit))
	s.addCommitMsg(createMessage("B", MessageReq_Commit))
	s.addCommitMsg(createMessage("C", MessageReq_Commit))
	committedSeals := s.getCommittedSeals()

	assert.Len(t, committedSeals, 3)
	processed := map[NodeID]struct{}{}
	for _, commSeal := range committedSeals {
		_, exists := processed[commSeal.NodeID]
		assert.False(t, exists) // all entries in committedSeals should be different
		processed[commSeal.NodeID] = struct{}{}
		el := s.committed[commSeal.NodeID]
		assert.NotNil(t, el)                                     // there should be entry in currentState.committed...
		assert.True(t, bytes.Equal(commSeal.Signature, el.Seal)) // ...and signatures should match
		assert.True(t, &commSeal.Signature != &el.Seal)
	}
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

func TestState_ToString(t *testing.T) {
	expectedMapping := map[State]string{
		AcceptState:      "AcceptState",
		RoundChangeState: "RoundChangeState",
		ValidateState:    "ValidateState",
		CommitState:      "CommitState",
		SyncState:        "SyncState",
		DoneState:        "DoneState",
	}

	for st, expected := range expectedMapping {
		assert.Equal(t, expected, st.String())
	}
}

func TestState_MaxFaultyVotingPower_EqualVotingPower(t *testing.T) {
	cases := []struct {
		nodesCount, faultyNodesCount uint
	}{
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
		pool := newTesterAccountPool(int(c.nodesCount))
		state, err := initState(pool)
		require.NoError(t, err)
		assert.Equal(t, c.faultyNodesCount, uint(state.MaxFaultyVotingPower()))
	}
}

func TestState_QuorumSize_EqualVotingPower(t *testing.T) {
	cases := []struct {
		nodesCount uint
		quorumSize uint64
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
		pool := newTesterAccountPool(int(c.nodesCount))
		state, err := initState(pool)
		require.NoError(t, err)
		assert.Equal(t, c.quorumSize, state.QuorumSize())
	}
}

func TestState_CalculateWeight_EqualVotingPower(t *testing.T) {
	cases := []struct {
		nodesCount uint
		weight     uint64
	}{
		{1, 1},
		{3, 3},
		{4, 4},
		{100, 100},
	}

	for _, c := range cases {
		pool := newTesterAccountPool(int(c.nodesCount))
		votingPower := pool.validatorSet().VotingPower()
		state, err := initState(pool)
		require.NoError(t, err)
		senders := make([]NodeID, 0)
		for nodeId := range votingPower {
			senders = append(senders, nodeId)
		}
		assert.Equal(t, c.weight, state.calculateVotingPower(senders))
	}
}

func TestState_MaxFaultyVotingPower_MixedVotingPower(t *testing.T) {
	cases := []struct {
		votingPower    map[NodeID]uint64
		maxFaultyNodes uint64
	}{
		// {map[NodeID]uint64{"A": 0, "B": 0, "C": 0}, 0},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 5},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 6},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 33},
	}
	for _, c := range cases {
		pool := newTesterAccountPool()
		pool.add(getValidatorIds(c.votingPower)...)
		state, err := initState(pool, c.votingPower)
		require.NoError(t, err)
		assert.Equal(t, c.maxFaultyNodes, state.MaxFaultyVotingPower())
	}
}

func TestState_QuorumSize_MixedVotingPower(t *testing.T) {
	cases := []struct {
		votingPower map[NodeID]uint64
		quorumSize  uint64
	}{
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 13},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 11},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 67},
	}
	for _, c := range cases {
		pool := newTesterAccountPool()
		pool.add(getValidatorIds(c.votingPower)...)
		state, err := initState(pool, c.votingPower)
		require.NoError(t, err)
		assert.Equal(t, c.quorumSize, state.QuorumSize())
	}
}

func TestState_CalculateWeight_MixedVotingPower(t *testing.T) {
	cases := []struct {
		votingPower map[NodeID]uint64
		quorumSize  uint64
	}{
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 20},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 16},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 100},
	}
	for _, c := range cases {
		pool := newTesterAccountPool()
		pool.add(getValidatorIds(c.votingPower)...)
		state, err := initState(pool, c.votingPower)
		require.NoError(t, err)
		senders := make([]NodeID, len(c.votingPower))
		i := 0
		for nodeId := range c.votingPower {
			senders[i] = nodeId
			i++
		}
		assert.Equal(t, c.quorumSize, state.calculateVotingPower(senders))
	}
}

func getValidatorIds(votingPowers map[NodeID]uint64) []string {
	validatorIds := make([]string, 0)
	for nodeId := range votingPowers {
		validatorIds = append(validatorIds, string(nodeId))
	}
	return validatorIds
}

type signDelegate func([]byte) ([]byte, error)
type testerAccount struct {
	alias  string
	priv   *ecdsa.PrivateKey
	signFn signDelegate
}

func (t *testerAccount) NodeID() NodeID {
	return NodeID(t.alias)
}

func (t *testerAccount) Sign(b []byte) ([]byte, error) {
	if t.signFn != nil {
		return t.signFn(b)
	}
	return nil, nil
}

type testerAccountPool struct {
	accounts []*testerAccount
}

func newTesterAccountPool(num ...int) *testerAccountPool {
	t := &testerAccountPool{
		accounts: []*testerAccount{},
	}
	if len(num) == 1 {
		for i := 0; i < num[0]; i++ {
			t.accounts = append(t.accounts, &testerAccount{
				alias: strconv.Itoa(i),
				priv:  generateKey(),
			})
		}
	}
	return t
}

func (ap *testerAccountPool) add(accounts ...string) {
	for _, account := range accounts {
		if acct := ap.get(account); acct != nil {
			continue
		}
		ap.accounts = append(ap.accounts, &testerAccount{
			alias: account,
			priv:  generateKey(),
		})
	}
}

func (ap *testerAccountPool) get(name string) *testerAccount {
	for _, account := range ap.accounts {
		if account.alias == name {
			return account
		}
	}
	return nil
}

func (ap *testerAccountPool) validatorSet(votingPower ...map[NodeID]uint64) ValidatorSet {
	validatorIds := []NodeID{}
	for _, acc := range ap.accounts {
		validatorIds = append(validatorIds, NodeID(acc.alias))
	}

	var votingPowerMap map[NodeID]uint64
	if len(votingPower) > 0 {
		votingPowerMap = votingPower[0]
	} else {
		votingPowerMap = CreateEqualWeightValidatorsMap(validatorIds)
	}

	return NewValStringStub(validatorIds, votingPowerMap)
}

// Helper function which enables creation of MessageReq.
// Note: sender needs to be seeded to the pool before invoking, otherwise senderId will be set to provided sender parameter.
func (ap *testerAccountPool) createMessage(sender string, messageType MsgType, round ...uint64) *MessageReq {
	poolSender := ap.get(sender)
	if poolSender != nil {
		sender = poolSender.alias
	}
	return createMessage(sender, messageType, round...)
}

func generateKey() *ecdsa.PrivateKey {
	prv, err := ecdsa.GenerateKey(elliptic.P384(), crand.Reader)
	if err != nil {
		panic(err)
	}
	return prv
}

func initState(accountPool *testerAccountPool, votingPower ...map[NodeID]uint64) (*state, error) {
	s := newState()
	var votingPowerMap map[NodeID]uint64
	if len(votingPower) > 0 {
		votingPowerMap = votingPower[0]
	} else {
		validatorIds := []NodeID{}
		for _, acc := range accountPool.accounts {
			validatorIds = append(validatorIds, NodeID(acc.alias))
		}
		votingPowerMap = CreateEqualWeightValidatorsMap(validatorIds)
	}
	s.validators = accountPool.validatorSet(votingPowerMap)
	err := s.initializeVotingInfo()
	if err != nil {
		return nil, err
	}
	return s, nil
}

func newMockValidatorSet(validatorIds []string) ValidatorSet {
	validatorNodeIds := []NodeID{}
	for _, id := range validatorIds {
		validatorNodeIds = append(validatorNodeIds, NodeID(id))
	}
	return NewValStringStub(validatorNodeIds, CreateEqualWeightValidatorsMap(validatorNodeIds))
}
