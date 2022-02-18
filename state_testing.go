package pbft

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"fmt"
	mrand "math/rand"
	"strconv"
)

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

func (ap *testerAccountPool) validatorSet() ValidatorSet {
	validatorIds := []NodeID{}
	for _, acc := range ap.accounts {
		validatorIds = append(validatorIds, NodeID(acc.alias))
	}
	return convertToMockValidatorSet(validatorIds)
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
