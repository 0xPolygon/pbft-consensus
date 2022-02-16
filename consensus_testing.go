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
)

var (
	mockProposal  = []byte{0x1, 0x2, 0x3}
	mockProposal1 = []byte{0x1, 0x2, 0x3, 0x4}
)

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
		WithLogger(log.New(loggerOutput, "", log.LstdFlags)),
		WithRoundTimeout(func(u uint64) time.Duration { return time.Millisecond }))

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

func (m *mockBackend) Init() {
}
