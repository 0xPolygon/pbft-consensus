package ibft

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
		pool := newTesterAccountPool(int(c.Network))
		vals := pool.ValidatorSet()
		assert.Equal(t, vals.MaxFaultyNodes(), int(c.Faulty))
	}
}

func TestState_AddMessages(t *testing.T) {
	pool := newTesterAccountPool()
	pool.add("A", "B", "C", "D")

	c := newState()
	// c.validators = pool.ValidatorSet()

	msg := func(acct string, typ MsgType, round ...uint64) *MessageReq {
		msg := &MessageReq{
			From: pool.get(acct).Address(),
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

func (t *testerAccount) Address() NodeID {
	return ""
	// return crypto.PubKeyToAddress(&t.priv.PublicKey)
}

func (t *testerAccount) Sign(b []byte) ([]byte, error) {
	// h, _ = writeSeal(t.priv, h)
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

func (ap *testerAccountPool) ValidatorSet() ValidatorSet {
	v := ValidatorSet{}
	for _, i := range ap.accounts {
		v = append(v, i.Address())
	}
	return v
}

func generateKey() *ecdsa.PrivateKey {
	prv, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		panic(err)
	}
	return prv
}
