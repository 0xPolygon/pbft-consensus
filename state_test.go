package pbft

import (
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
func createMessage(sender NodeID, messageType MsgType, view *View) *MessageReq {
	if view == nil {
		view = ViewMsg(1, 0)
	}
	msg := &MessageReq{
		From: sender,
		Type: messageType,
		View: view,
	}
	switch msg.Type {
	case MessageReq_Preprepare:
		msg.Proposal = mockProposal
		msg.Hash = digest

	case MessageReq_Commit:
		seal := make([]byte, 2)
		mrand.Read(seal)
		msg.Seal = seal
	}
	return msg
}

func TestState_AddMessages(t *testing.T) {
	pool := newTesterAccountPool()
	validatorIds := []NodeID{"A", "B", "C", "D"}
	pool.addAccounts(CreateEqualVotingPowerMap(validatorIds))

	s, err := initState(pool)
	require.NoError(t, err)
	s.validators = pool.validatorSet()

	// Send message from node which is not amongst validator nodes
	s.addMessage(createMessage("E", MessageReq_Prepare, ViewMsg(1, 0)))
	assert.Empty(t, s.committed.messageMap)
	assert.Empty(t, s.prepared.messageMap)
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
		assert.Equal(t, s.validators.Len(), msgsPerRound.length())
	}
}

func TestState_MaxRound_Found(t *testing.T) {
	const (
		validatorsCount = 5
		roundsCount     = 6
	)

	validatorIds := make([]NodeID, validatorsCount)
	for i := 0; i < validatorsCount; i++ {
		validatorId := fmt.Sprintf("validator_%d", i)
		validatorIds[i] = NodeID(validatorId)
	}
	pool := newTesterAccountPool()
	pool.addAccounts(CreateEqualVotingPowerMap(validatorIds))
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

	validatorIds := make([]NodeID, validatorsCount)
	for i := 0; i < validatorsCount; i++ {
		validatorIds[i] = NodeID(fmt.Sprintf("validator_%d", i))
	}
	pool := newTesterAccountPool()
	pool.addAccounts(CreateEqualVotingPowerMap(validatorIds))
	s, err := initState(pool)
	require.NoError(t, err)

	// Send wrong message type from some validator, whereas roundMessages map is empty
	s.addMessage(createMessage(validatorIds[0], MessageReq_Preprepare, ViewMsg(1, 1)))

	maxRound, found := s.maxRound()
	assert.Equal(t, maxRound, uint64(0))
	assert.Equal(t, found, false)

	// Seed insufficient "RoundChange" messages count, so that maxRound isn't going to be found
	for round := range validatorIds {
		if round%2 == 0 {
			// Each even round should populate more than one "RoundChange" messages, but just enough that we don't reach census (max faulty nodes+1)
			for i := 0; i < int(s.getMaxFaultyVotingPower()); i++ {
				s.addMessage(createMessage(validatorIds[mrand.Intn(validatorsCount)], MessageReq_RoundChange, ViewMsg(1, uint64(round))))
			}
		} else {
			s.addMessage(createMessage(validatorIds[mrand.Intn(validatorsCount)], MessageReq_RoundChange, ViewMsg(1, uint64(round))))
		}
	}

	maxRound, found = s.maxRound()
	assert.Equal(t, uint64(0), maxRound)
	assert.Equal(t, false, found)
}

func TestState_AddRoundMessage(t *testing.T) {
	s := newState()
	validatorIds := []NodeID{"A", "B"}
	s.validators = NewValStringStub(validatorIds, CreateEqualVotingPowerMap(validatorIds))

	// commit message isn't added to the round change messages queue
	s.addRoundChangeMsg(createMessage("A", MessageReq_Commit, ViewMsg(1, 0)))
	assert.Empty(t, s.roundMessages)
	assert.Nil(t, s.roundMessages[0])

	s.addRoundChangeMsg(createMessage("A", MessageReq_RoundChange, ViewMsg(1, 0)))
	s.addRoundChangeMsg(createMessage("A", MessageReq_RoundChange, ViewMsg(1, 1)))
	s.addRoundChangeMsg(createMessage("A", MessageReq_RoundChange, ViewMsg(1, 2)))

	s.addRoundChangeMsg(createMessage("B", MessageReq_RoundChange, ViewMsg(1, 2)))
	assert.Equal(t, 2, s.roundMessages[2].length())

	s.addRoundChangeMsg(createMessage("B", MessageReq_RoundChange, ViewMsg(1, 3)))
	assert.Len(t, s.roundMessages, 4)

	assert.Empty(t, s.prepared.messageMap)
	assert.Empty(t, s.committed.messageMap)
}

func TestState_addPrepared(t *testing.T) {
	s := newState()
	validatorIds := []NodeID{"A", "B"}
	s.validators = NewValStringStub(validatorIds, CreateEqualVotingPowerMap(validatorIds))

	s.addPrepareMsg(createMessage("A", MessageReq_Commit, ViewMsg(1, 1)))
	assert.Equal(t, 0, s.prepared.length())

	s.addPrepareMsg(createMessage("A", MessageReq_Prepare, ViewMsg(1, 1)))
	s.addPrepareMsg(createMessage("B", MessageReq_Prepare, ViewMsg(1, 1)))

	assert.Equal(t, len(validatorIds), s.prepared.length())
	assert.True(t, s.committed.length() == 0)
	assert.Empty(t, s.roundMessages)
}

func TestState_addCommitted(t *testing.T) {
	s := newState()
	validatorIds := []NodeID{"A", "B"}
	s.validators = NewValStringStub(validatorIds, CreateEqualVotingPowerMap(validatorIds))

	s.addCommitMsg(createMessage("A", MessageReq_Prepare, ViewMsg(1, 1)))
	assert.True(t, s.committed.length() == 0)

	s.addCommitMsg(createMessage("A", MessageReq_Commit, ViewMsg(1, 1)))
	s.addCommitMsg(createMessage("B", MessageReq_Commit, ViewMsg(1, 1)))

	assert.Equal(t, len(validatorIds), s.committed.length())
	assert.True(t, s.prepared.length() == 0)
	assert.Empty(t, s.roundMessages)
}

func TestState_Copy(t *testing.T) {
	originalMsg := createMessage("A", MessageReq_Preprepare, ViewMsg(1, 0))
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

	s.lock(0)
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
	pool.addAccounts(CreateEqualVotingPowerMap([]NodeID{"A", "B", "C", "D", "E"}))

	s := newState()
	s.validators = pool.validatorSet()

	s.addCommitMsg(createMessage("A", MessageReq_Commit, ViewMsg(1, 0)))
	s.addCommitMsg(createMessage("B", MessageReq_Commit, ViewMsg(1, 0)))
	s.addCommitMsg(createMessage("C", MessageReq_Commit, ViewMsg(1, 0)))
	committedSeals := s.getCommittedSeals()

	assert.Len(t, committedSeals, 3)
	processed := map[NodeID]struct{}{}
	for _, commSeal := range committedSeals {
		_, exists := processed[commSeal.NodeID]
		assert.False(t, exists) // all entries in committedSeals should be different
		processed[commSeal.NodeID] = struct{}{}
		msg := s.committed.messageMap[commSeal.NodeID]
		assert.NotNil(t, msg)                         // there should be entry in currentState.committed...
		assert.Equal(t, commSeal.Signature, msg.Seal) // ...and signatures should match
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
		assert.Equal(t, c.faultyNodesCount, uint(state.getMaxFaultyVotingPower()))
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
		assert.Equal(t, c.quorumSize, state.getQuorumSize())
	}
}

func TestState_MaxFaultyVotingPower_MixedVotingPower(t *testing.T) {
	cases := []struct {
		votingPower    map[NodeID]uint64
		maxFaultyNodes uint64
	}{
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 6}, 5},
		{map[NodeID]uint64{"A": 5, "B": 5, "C": 5, "D": 5}, 6},
		{map[NodeID]uint64{"A": 50, "B": 25, "C": 10, "D": 15}, 33},
	}
	for _, c := range cases {
		pool := newTesterAccountPool()
		pool.addAccounts(c.votingPower)
		state, err := initState(pool)
		require.NoError(t, err)
		assert.Equal(t, c.maxFaultyNodes, state.getMaxFaultyVotingPower())
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
		pool.addAccounts(c.votingPower)
		state, err := initState(pool)
		require.NoError(t, err)
		assert.Equal(t, c.quorumSize, state.getQuorumSize())
	}
}

type signDelegate func([]byte) ([]byte, error)
type testerAccount struct {
	alias       NodeID
	priv        *ecdsa.PrivateKey
	votingPower uint64
	signFn      signDelegate
}

func (t *testerAccount) NodeID() NodeID {
	return t.alias
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
				alias:       NodeID(strconv.Itoa(i)),
				priv:        generateKey(),
				votingPower: 1,
			})
		}
	}
	return t
}

func (ap *testerAccountPool) addAccounts(votingPowerMap map[NodeID]uint64) {
	for alias, votingPower := range votingPowerMap {
		if acct := ap.get(alias); acct != nil {
			continue
		}
		ap.accounts = append(ap.accounts, &testerAccount{
			alias:       alias,
			priv:        generateKey(),
			votingPower: votingPower,
		})
	}
}

func (ap *testerAccountPool) get(alias NodeID) *testerAccount {
	for _, account := range ap.accounts {
		if account.alias == alias {
			return account
		}
	}
	return nil
}

func (ap *testerAccountPool) validatorSet() ValidatorSet {
	validatorIds := make([]NodeID, len(ap.accounts))
	votingPowerMap := make(map[NodeID]uint64, len(ap.accounts))
	for i, acc := range ap.accounts {
		validatorIds[i] = acc.alias
		votingPowerMap[acc.alias] = acc.votingPower
	}
	return NewValStringStub(validatorIds, votingPowerMap)
}

// Helper function which enables creation of MessageReq.
// Note: sender needs to be seeded to the pool before invoking, otherwise senderId will be set to provided sender parameter.
func (ap *testerAccountPool) createMessage(sender NodeID, messageType MsgType, round ...uint64) *MessageReq {
	poolSender := ap.get(sender)
	if poolSender != nil {
		sender = poolSender.alias
	}
	r := uint64(0)
	if len(round) == 1 {
		r = round[0]
	}
	return createMessage(sender, messageType, ViewMsg(1, r))
}

func generateKey() *ecdsa.PrivateKey {
	prv, err := ecdsa.GenerateKey(elliptic.P384(), crand.Reader)
	if err != nil {
		panic(err)
	}
	return prv
}

func initState(accountPool *testerAccountPool) (*state, error) {
	s := newState()
	s.validators = accountPool.validatorSet()
	err := s.initializeVotingInfo()
	if err != nil {
		return nil, err
	}
	return s, nil
}
