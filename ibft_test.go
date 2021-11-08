package ibft

import (
	"context"
	"crypto/sha1"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTransition_ValidateState_Prepare(t *testing.T) {
	t.Skip()

	// we receive enough prepare messages to lock and commit the block
	i := newMockIbft(t, []string{"A", "B", "C", "D"}, "A")
	i.setState(ValidateState)

	i.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	i.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	// repeated message is not included
	i.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	i.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Prepare,
		View: ViewMsg(1, 0),
	})
	i.Close()

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence:    1,
		state:       ValidateState,
		prepareMsgs: 3,
		commitMsgs:  1, // A commit message
		locked:      true,
		outgoing:    1, // A commit message
	})
}

func TestTransition_ValidateState_CommitFastTrack(t *testing.T) {
	t.Skip()

	// we can directly receive the commit messages and fast track to the commit state
	// even when we do not have yet the preprepare messages
	i := newMockIbft(t, []string{"A", "B", "C", "D"}, "A")

	// seal := hex.EncodeToHex(make([]byte, IstanbulExtraSeal))

	i.setState(ValidateState)
	i.state.view = ViewMsg(1, 0)
	// i.state.block = i.DummyBlock()
	i.state.locked = true

	i.emitMsg(&MessageReq{
		From: "A",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
		//Seal: seal,
	})
	i.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
		//Seal: seal,
	})
	i.emitMsg(&MessageReq{
		From: "B",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
		//Seal: seal,
	})
	i.emitMsg(&MessageReq{
		From: "C",
		Type: MessageReq_Commit,
		View: ViewMsg(1, 0),
		//Seal: seal,
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence:   1,
		commitMsgs: 3,
		outgoing:   1,
		locked:     false, // unlock after commit
	})
}

func TestTransition_AcceptState_ToSyncState(t *testing.T) {
	// we are in AcceptState and we are not in the validators list
	// means that we have been removed as validator, move to sync state
	i := newMockIbft(t, []string{"A", "B", "C", "D"}, "")
	i.setState(AcceptState)
	i.Close()

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    SyncState,
	})
}

var (
	mockProposal  = []byte{0x1, 0x2, 0x3}
	mockProposal1 = []byte{0x1, 0x2, 0x3, 0x4}
)

func TestTransition_AcceptState_Proposer_ProposeBlock(t *testing.T) {
	// we are in AcceptState and we are the proposer, it needs to:
	// 1. create a block
	// 2. wait for the delay
	// 3. send a preprepare message
	// 4. send a prepare message
	// 5. move to ValidateState

	i := newMockIbft(t, []string{"A", "B", "C", "D"}, "A")
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
	// it needs to send the locked block again
	i := newMockIbft(t, []string{"A", "B", "C", "D"}, "A")
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

	/*
		if i.state.block.Number() != 10 {
			t.Fatal("bad block")
		}
	*/
}

func TestTransition_AcceptState_Validator_VerifyCorrect(t *testing.T) {
	i := newMockIbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	//block := i.DummyBlock()
	//header, err := writeSeal(i.pool.get("A").priv, block.Header)
	//assert.NoError(t, err)
	//block.Header = header

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

	i := newMockIbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	block := mockProposal
	// block.Header.MixHash = types.Hash{} // invalidates the block

	//header, err := writeSeal(i.pool.get("A").priv, block.Header)
	//assert.NoError(t, err)
	//block.Header = header

	// A sends the message
	i.emitMsg(&MessageReq{
		From:     "A",
		Type:     MessageReq_Preprepare,
		Proposal: block,
		View:     ViewMsg(1, 0),
	})

	i.runCycle(context.Background())

	i.expect(expectResult{
		sequence: 1,
		state:    RoundChangeState,
		err:      errBlockVerificationFailed,
	})
}

func TestTransition_AcceptState_Validator_ProposerInvalid(t *testing.T) {
	i := newMockIbft(t, []string{"A", "B", "C"}, "B")
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

	i := newMockIbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	// locked block

	//block.Header.Number = 1
	//block.Header.ComputeHash()

	i.state.proposal = &Proposal{
		Data: mockProposal,
	}
	i.state.locked = true

	// proposed block
	//block1.Header.Number = 2
	//block1.Header.ComputeHash()

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
		err:      errIncorrectBlockLocked,
	})
}

func TestTransition_AcceptState_Validator_LockCorrect(t *testing.T) {
	i := newMockIbft(t, []string{"A", "B", "C"}, "B")
	i.state.view = ViewMsg(1, 0)
	i.setState(AcceptState)

	// locked block
	proposal := mockProposal
	//block.Header.Number = 1
	//block.Header.ComputeHash()

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

func TestTransition_RoundChangeState_CatchupRound(t *testing.T) {
	m := newMockIbft(t, []string{"A", "B", "C", "D"}, "A")
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
	m := newMockIbft(t, []string{"A", "B", "C", "D"}, "A")

	m.forceTimeout()
	m.setState(RoundChangeState)
	m.Close()

	// increases to round 1 at the beginning of the round and sends
	// one RoundChange message.
	// After the timeout, it increases to round 2 and sends another
	/// RoundChange message.
	m.runCycle(context.Background())

	m.expect(expectResult{
		sequence: 1,
		round:    2,
		outgoing: 2, // two round change messages
		state:    RoundChangeState,
	})
}

func TestTransition_RoundChangeState_WeakCertificate(t *testing.T) {
	m := newMockIbft(t, []string{"A", "B", "C", "D", "E", "F", "G"}, "A")

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
	m := newMockIbft(t, []string{"A", "B"}, "A")
	m.Close()

	m.state.err = errBlockVerificationFailed

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
	m := newMockIbft(t, []string{"A", "B"}, "A")
	m.Close()

	m.state.view.Sequence = 1

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
	m := newMockIbft(t, []string{"A", "B", "C"}, "A")
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

type mockIbft struct {
	t *testing.T
	*Ibft

	//blockchain *blockchain.Blockchain
	pool    *testerAccountPool
	respMsg []*MessageReq

	mockB *mockB
}

/*
func (m *mockIbft) DummyBlock() []byte {
	return nil

		//parent, _ := m.blockchain.GetHeaderByNumber(0)
		block := &types.Block{
			Header: &types.Header{
				//ExtraData:  parent.ExtraData,
				//MixHash:    IstanbulDigest,
				Sha3Uncles: types.EmptyUncleHash,
			},
		}
		return block

}
*/

/*
func (m *mockIbft) Header() *types.Header {
	return m.blockchain.Header()
}

func (m *mockIbft) GetHeaderByNumber(i uint64) (*types.Header, bool) {
	return m.blockchain.GetHeaderByNumber(i)
}

func (m *mockIbft) WriteBlocks(blocks []*types.Block) error {
	return nil
}
*/

func (m *mockIbft) emitMsg(msg *MessageReq) {
	// convert the address from the address pool
	// from := m.pool.get(string(msg.From)).Address()
	// msg.From = from

	m.Ibft.pushMessage(msg)
}

func (m *mockIbft) addMessage(msg *MessageReq) {
	// convert the address from the address pool
	//from := m.pool.get(string(msg.From)).Address()
	//msg.From = from

	m.state.addMessage(msg)
}

func (m *mockIbft) Gossip(msg *MessageReq) error {
	m.respMsg = append(m.respMsg, msg)
	return nil
}

func newMockIbft(t *testing.T, accounts []string, account string) *mockIbft {
	pool := newTesterAccountPool()
	pool.add(accounts...)

	valsAsNode := []NodeID{}
	for _, i := range accounts {
		valsAsNode = append(valsAsNode, NodeID(i))
	}

	fmt.Println("--cc")
	fmt.Println(valsAsNode)

	m := &mockIbft{
		t:    t,
		pool: pool,
		//blockchain: blockchain.TestBlockchain(t, pool.genesis()),
		respMsg: []*MessageReq{},
		mockB: &mockB{
			validators: valString(valsAsNode),
		},
	}

	var acct *testerAccount
	if account == "" {
		// account not in validator set, create a new one that is not part
		// of the genesis
		pool.add("xx")
		acct = pool.get("xx")
	} else {
		acct = pool.get(account)
	}
	ibft := &Ibft{
		logger: log.New(os.Stdout, "", log.LstdFlags),
		//logger:           hclog.NewNullLogger(),
		//config: &consensus.Config{},
		//blockchain:       m,
		validator: acct,
		closeCh:   make(chan struct{}),
		updateCh:  make(chan struct{}),
		//operator:         &operator{},
		state:    newState(),
		inter:    m.mockB,
		msgQueue: newMsgQueue(),
		//epochSize:        DefaultEpochSize,
	}

	// by default set the state to (1, 0)
	ibft.state.view = ViewMsg(1, 0)

	m.Ibft = ibft

	//assert.NoError(t, ibft.setupSnapshot())
	//assert.NoError(t, ibft.createKey())

	//set the initial validators frrom the snapshot
	xx := valString(valsAsNode)
	ibft.state.validators = &xx

	m.Ibft.transport = m
	return m
}

func (i *mockIbft) setProposal(p *Proposal) {
	i.mockB.proposal = p
}

type expectResult struct {
	state    IbftState
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

func (m *mockIbft) expect(res expectResult) {
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

type mockB struct {
	validators valString
	proposal   *Proposal
}

func (m *mockB) Hash(p []byte) ([]byte, error) {
	h := sha1.New()
	h.Write(p)
	return h.Sum(nil), nil
}

func (m *mockB) BuildBlock() (*Proposal, error) {
	if m.proposal == nil {
		panic("add a proposal in the test")
	}
	return m.proposal, nil
}

func (m *mockB) Validate(proposal []byte) ([]byte, error) {
	return nil, nil
}

func (m *mockB) Insert(proposal []byte, committedSeals [][]byte) error {
	// TODO
	return nil
}

func (m *mockB) ValidatorSet() (*Snapshot, error) {
	snap := &Snapshot{
		ValidatorSet: &m.validators,
	}
	return snap, nil
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

func (v *valString) Index(addr NodeID) int {
	for indx, i := range *v {
		if i == addr {
			return indx
		}
	}

	return -1
}

func (v *valString) Includes(id NodeID) bool {
	for _, i := range *v {
		if i == id {
			return true
		}
	}
	return false
}

func (v *valString) Len() int {
	return len(*v)
}
