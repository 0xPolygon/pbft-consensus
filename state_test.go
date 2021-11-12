package ibft

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestState_FaultyNodes(t *testing.T) {
	cases := []struct {
		Network, Faulty uint64
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
	}
	for _, c := range cases {
		acct := []string{}
		for i := 0; i < int(c.Network); i++ {
			acct = append(acct, fmt.Sprintf("acct_%d", i))
		}
		s := newState()
		s.validators = newMockValidatorSet(acct)

		assert.Equal(t, s.MaxFaultyNodes(), int(c.Faulty))
	}
}

func TestState_AddMessages(t *testing.T) {
	pool := newTesterAccountPool()
	pool.add("A", "B", "C", "D")

	c := newState()
	c.validators = pool.validatorSet()

	msg := func(acct string, typ MsgType, round ...uint64) *MessageReq {
		msg := &MessageReq{
			From: NodeID(pool.get(acct).alias),
			Type: typ,
			View: &View{Round: 0},
		}
		r := uint64(0)
		if len(round) > 0 {
			r = round[0]
		}
		msg.View.Round = r
		return msg
	}

	// -- test committed messages --
	c.addMessage(msg("A", MessageReq_Commit))
	c.addMessage(msg("B", MessageReq_Commit))
	c.addMessage(msg("B", MessageReq_Commit))

	assert.Equal(t, c.numCommitted(), 2)

	// -- test prepare messages --
	c.addMessage(msg("C", MessageReq_Prepare))
	c.addMessage(msg("C", MessageReq_Prepare))
	c.addMessage(msg("D", MessageReq_Prepare))

	assert.Equal(t, c.numPrepared(), 2)
}

type testerAccount struct {
	alias string
	priv  *ecdsa.PrivateKey
}

func (t *testerAccount) NodeID() NodeID {
	return NodeID(t.alias)
}

func (t *testerAccount) Sign(b []byte) ([]byte, error) {
	// TODO:
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
	for _, i := range ap.accounts {
		if i.alias == name {
			return i
		}
	}
	return nil
}

func (ap *testerAccountPool) validatorSet() ValidatorSet {
	acct := []string{}
	for _, i := range ap.accounts {
		acct = append(acct, i.alias)
	}
	return newMockValidatorSet(acct)
}

func generateKey() *ecdsa.PrivateKey {
	prv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return prv
}

func newMockValidatorSet(a []string) ValidatorSet {
	valsAsNode := []NodeID{}
	for _, i := range a {
		valsAsNode = append(valsAsNode, NodeID(i))
	}
	set := valString(valsAsNode)
	return &set
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
