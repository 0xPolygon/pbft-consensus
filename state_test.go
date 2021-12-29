package pbft

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"fmt"
	mrand "math/rand"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func init() {
	mrand.Seed(time.Now().UnixNano())
}

// Generate predefined number of validator node ids.
// Node id is generated with a given prefixNodeId, followed by underscore and an index.
func generateValidatorNodes(nodesCount int, prefixNodeId string) []NodeID {
	validatorNodeIds := []NodeID{}
	for i := 0; i < nodesCount; i++ {
		validatorNodeIds = append(validatorNodeIds, NodeID(fmt.Sprintf("%s_%d", prefixNodeId, i)))
	}
	return validatorNodeIds
}

// Helper function which enables creation of MessageReq.
func createMessage(sender string, messageType MsgType, round ...uint64) *MessageReq {
	return createMessagePoolSender(sender, messageType, nil, round...)
}

// Helper function which enables creation of MessageReq.
// Note: sender needs to be seeded to the pool before invoking, otherwise senderId will be set to provided sender parameter.
func createMessagePoolSender(sender string, messageType MsgType, pool *testerAccountPool, round ...uint64) *MessageReq {
	senderId := sender
	if pool != nil {
		senderId = pool.get(sender).alias
	}
	seal := make([]byte, 2)
	mrand.Read(seal)

	r := uint64(0)
	if len(round) > 0 {
		r = round[0]
	}

	msg := &MessageReq{
		From:     NodeID(senderId),
		Type:     messageType,
		View:     &View{Round: r},
		Seal:     seal,
		Proposal: mockProposal,
	}
	return msg
}

func TestState_FaultyNodesCount(t *testing.T) {
	cases := []struct {
		TotalNodesCount, FaultyNodesCount int
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
		{100, 33},
	}
	for _, c := range cases {
		validatorNodeIds := generateValidatorNodes(c.TotalNodesCount, "validator")
		s := newState()
		s.validators = convertToMockValidatorSet(validatorNodeIds)

		assert.Equal(t, s.MaxFaultyNodes(), c.FaultyNodesCount)
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
		validatorNodeIds := generateValidatorNodes(c.TotalNodesCount, "validator")
		s := newState()
		s.validators = convertToMockValidatorSet(validatorNodeIds)

		assert.Equal(t, s.NumValid(), c.ValidNodesCount)
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
	s.addMessage(createMessagePoolSender("A", MessageReq_Commit, pool))
	s.addMessage(createMessagePoolSender("B", MessageReq_Commit, pool))
	s.addMessage(createMessagePoolSender("B", MessageReq_Commit, pool))

	assert.Equal(t, s.numCommitted(), 2)

	// -- test prepare messages --
	s.addMessage(createMessagePoolSender("C", MessageReq_Prepare, pool))
	s.addMessage(createMessagePoolSender("C", MessageReq_Prepare, pool))
	s.addMessage(createMessagePoolSender("D", MessageReq_Prepare, pool))

	assert.Equal(t, s.numPrepared(), 2)

	// -- test round change messages --
	rounds := 2
	for round := 0; round < rounds; round++ {
		for i := 0; i < s.validators.Len(); i++ {
			s.addMessage(createMessagePoolSender(validatorIds[i], MessageReq_RoundChange, pool, uint64(round)))
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
	validatorIds := []string{"validator_1", "validator_2"}
	s.validators = newMockValidatorSet(validatorIds)

	roundMessageSize := s.AddRoundMessage(createMessage(validatorIds[0], MessageReq_Commit, 0))
	assert.Equal(t, 0, roundMessageSize)
	assert.Equal(t, 0, len(s.roundMessages))

	s.AddRoundMessage(createMessage(validatorIds[0], MessageReq_RoundChange, 0))
	s.AddRoundMessage(createMessage(validatorIds[0], MessageReq_RoundChange, 1))
	s.AddRoundMessage(createMessage(validatorIds[0], MessageReq_RoundChange, 2))

	roundMessageSize = s.AddRoundMessage(createMessage(validatorIds[1], MessageReq_RoundChange, 2))
	assert.Equal(t, 2, roundMessageSize)

	s.AddRoundMessage(createMessage(validatorIds[1], MessageReq_RoundChange, 3))
	assert.Equal(t, 4, len(s.roundMessages))

	assert.Empty(t, s.prepared)
	assert.Empty(t, s.committed)
}

func TestState_addPrepared(t *testing.T) {
	s := newState()
	validatorIds := []string{"validator_1", "validator_2"}
	s.validators = newMockValidatorSet(validatorIds)

	s.addPrepared(createMessage(validatorIds[0], MessageReq_Commit))
	assert.Equal(t, 0, len(s.prepared))

	s.addPrepared(createMessage(validatorIds[0], MessageReq_Prepare))
	s.addPrepared(createMessage(validatorIds[1], MessageReq_Prepare))

	assert.Equal(t, len(validatorIds), len(s.prepared))
	assert.Empty(t, s.committed)
	assert.Empty(t, s.roundMessages)
}

func TestState_addCommitted(t *testing.T) {
	s := newState()
	validatorIds := []string{"validator_1", "validator_2"}
	s.validators = newMockValidatorSet(validatorIds)

	s.addCommitted(createMessage(validatorIds[0], MessageReq_Prepare))
	assert.Empty(t, 0, s.committed)

	s.addCommitted(createMessage(validatorIds[0], MessageReq_Commit))
	s.addCommitted(createMessage(validatorIds[1], MessageReq_Commit))

	assert.Equal(t, len(validatorIds), len(s.committed))
	assert.Empty(t, s.prepared)
	assert.Empty(t, s.roundMessages)
}

func TestState_Copy(t *testing.T) {
	originalMsg := createMessage("validator_1", MessageReq_Preprepare, 0)
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
	s := newState()
	validatorIds := []string{"A", "B"}
	s.validators = newMockValidatorSet(validatorIds)

	s.addCommitted(createMessage(validatorIds[0], MessageReq_Commit))
	s.addCommitted(createMessage(validatorIds[1], MessageReq_Commit))
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
	assert.Panics(t, func() { _ = MsgType(20).String() })
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
	assert.Panics(t, func() { _ = PbftState(20).String() })
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
	return ecdsa.SignASN1(crand.Reader, t.priv, b)
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

func (ap *testerAccountPool) validatorSet() ValidatorSet {
	validatorIds := []NodeID{}
	for _, acc := range ap.accounts {
		validatorIds = append(validatorIds, NodeID(acc.alias))
	}
	return convertToMockValidatorSet(validatorIds)
}

func generateKey() *ecdsa.PrivateKey {
	prv, err := ecdsa.GenerateKey(elliptic.P384(), crand.Reader)
	if err != nil {
		panic(err)
	}
	return prv
}

func newMockValidatorSet(validatorIds []string) ValidatorSet {
	validatorNodeIds := []NodeID{}
	for _, id := range validatorIds {
		validatorNodeIds = append(validatorNodeIds, NodeID(id))
	}
	return convertToMockValidatorSet(validatorNodeIds)
}

func convertToMockValidatorSet(validatorIds []NodeID) ValidatorSet {
	validatorSet := valString(validatorIds)
	return &validatorSet
}

type valString []NodeID

func (v *valString) CalcProposer(round uint64) NodeID {
	seed := uint64(0)

	offset := 0
	// add last proposer

	seed = uint64(offset) + round
	pick := seed % uint64(v.Len())

	return (*v)[pick]
}

func (v *valString) Index(id NodeID) int {
	for i, currentId := range *v {
		if currentId == id {
			return i
		}
	}

	return -1
}

func (v *valString) Includes(id NodeID) bool {
	for _, currentId := range *v {
		if currentId == id {
			return true
		}
	}
	return false
}

func (v *valString) Len() int {
	return len(*v)
}
