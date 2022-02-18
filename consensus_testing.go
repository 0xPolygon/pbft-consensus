package pbft

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"testing"
	"time"
)

var (
	mockProposal = []byte{0x1, 0x2, 0x3}
	digest       = []byte{0x1}

	mockProposal1 = []byte{0x1, 0x2, 0x3, 0x4}
	digest1       = []byte{0x2}
)

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

type gossipDelegate func(*MessageReq) error

func (m *mockPbft) HookGossipHandler(gossipFn gossipDelegate) {
	m.gossipFn = gossipFn
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

type backendConfigCallback func(backend *mockBackend)

func newMockPbft(
	t *testing.T,
	accounts []string,
	account string,
	backendConfigCallback ...backendConfigCallback,
) *mockPbft {
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
		WithRoundTimeout(func(u uint64) time.Duration { return time.Millisecond }))

	// initialize backend mock
	backend := newMockBackend(accounts, m)
	if len(backendConfigCallback) == 1 && backendConfigCallback[0] != nil {
		backendConfigCallback[0](backend)
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
		validators: newMockValidatorSet(validatorIds).(*valString),
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
type validateDelegate func(proposal *Proposal) error
type isStuckDelegate func(uint64) (uint64, bool)
type insertDelegate func(proposal *SealedProposal) error

type mockBackend struct {
	mock            *mockPbft
	validators      *valString
	buildProposalFn buildProposalDelegate
	validateFn      validateDelegate
	isStuckFn       isStuckDelegate
	insertFn        insertDelegate
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

func (m *mockBackend) HookInsertHandler(insert insertDelegate) *mockBackend {
	m.insertFn = insert
	return m
}

func (m *mockBackend) ValidateCommit(_ NodeID, _ []byte) error {
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
	if m.insertFn != nil {
		return m.insertFn(pp)
	}

	return nil
}

func (m *mockBackend) ValidatorSet() ValidatorSet {
	return m.validators
}

func (m *mockBackend) Init() {
}

type mockPBFTCluster struct {
	nodes []*mockPbft
}

// newMockPBFTClusterWithMap creates a new mock PBFT cluster with set backends
func newMockPBFTClusterWithBackends(
	t *testing.T,
	nodePrefix string,
	numNodes uint64,
	backendCallbackMap map[int]backendConfigCallback,
) *mockPBFTCluster {
	t.Helper()

	if numNodes < 1 {
		return nil
	}

	// Generate the node names
	nodeNames := generateMockClusterNames(nodePrefix, numNodes)

	// Instantiate each node in the cluster
	nodes := make([]*mockPbft, numNodes)
	for indx, nodeName := range nodeNames {
		// If the backend for the node is set, use it
		backendCallback, _ := backendCallbackMap[indx]
		nodes[indx] = newMockPbft(
			t,
			nodeNames,
			nodeName,
			backendCallback,
		)
	}

	return &mockPBFTCluster{
		nodes: nodes,
	}
}

// generateMockClusterNames generates node names using the specified prefix
func generateMockClusterNames(nodePrefix string, numNodes uint64) []string {
	nodeNames := make([]string, numNodes)
	for i := uint64(0); i < numNodes; i++ {
		nodeNames[i] = fmt.Sprintf("%s%d", nodePrefix, i)
	}

	return nodeNames
}

func generateRandomBytes(size int) ([]byte, error) {
	b := make([]byte, size)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (mc *mockPBFTCluster) runAcceptState() {
	runAcceptState(mc.nodes)
}

func runAcceptState(nodes []*mockPbft) {
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)

		go func(node *mockPbft) {
			defer wg.Done()

			node.runAcceptState(context.Background())
		}(node)
	}

	wg.Wait()
}

func (mc *mockPBFTCluster) runValidateState() {
	runValidateState(mc.nodes)
}

func runValidateState(nodes []*mockPbft) {
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)

		go func(node *mockPbft) {
			defer wg.Done()

			node.runValidateState(context.Background())
		}(node)
	}

	wg.Wait()
}

func (mc *mockPBFTCluster) runRoundChangeState() {
	runRoundChangeState(mc.nodes)
}

func runRoundChangeState(nodes []*mockPbft) {
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)

		go func(node *mockPbft) {
			defer wg.Done()

			node.runRoundChangeState(context.Background())
		}(node)
	}

	wg.Wait()
}

func (mc *mockPBFTCluster) runCommitState() {
	runCommitState(mc.nodes)
}

func runCommitState(nodes []*mockPbft) {
	var wg sync.WaitGroup

	for _, node := range nodes {
		wg.Add(1)

		go func(node *mockPbft) {
			defer wg.Done()

			node.runCommitState(context.Background())
		}(node)
	}

	wg.Wait()
}

func verifyAllInState(nodes []*mockPbft, state PbftState) error {
	for _, node := range nodes {
		if PbftState(node.state.state) != state {
			return fmt.Errorf(
				"node %s not in state %s",
				node.validator.NodeID(),
				state.String(),
			)
		}
	}

	return nil
}

func verifyAllInRound(nodes []*mockPbft, round uint64) error {
	for _, node := range nodes {
		if node.state.view.Round != round {
			return fmt.Errorf(
				"node %s not in round %d",
				node.validator.NodeID(),
				round,
			)
		}
	}

	return nil
}

func verifyAllAtSequence(nodes []*mockPbft, sequence uint64) error {
	for _, node := range nodes {
		if node.state.view.Sequence != sequence {
			return fmt.Errorf(
				"node %s not at sequence %d",
				node.validator.NodeID(),
				sequence,
			)
		}
	}

	return nil
}

type verifyProposalParams struct {
	proposal []byte
	proposer NodeID
}

func verifyAllSameProposal(
	nodes []*mockPbft,
	params verifyProposalParams,
) error {
	for _, node := range nodes {
		// Everyone is working with the same proposal
		if !bytes.Equal(node.state.proposal.Data, params.proposal) {
			return fmt.Errorf(
				"node %s doesn't have the same proposal",
				node.validator.NodeID(),
			)
		}

		// Everyone is working with the same proposal
		if node.state.proposer != params.proposer {
			return fmt.Errorf(
				"node %s doesn't have the same proposer",
				node.validator.NodeID(),
			)
		}

	}

	return nil
}

func hashProposalData(p []byte) []byte {
	h := sha1.New()
	h.Write(p)

	return h.Sum(nil)
}
