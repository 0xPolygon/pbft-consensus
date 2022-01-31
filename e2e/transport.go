package e2e

import (
	"math/rand"
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

type transport struct {
	nodes map[pbft.NodeID]transportHandler
	hook  transportHook
}

func (t *transport) addHook(hook transportHook) {
	t.hook = hook
}

type transportHandler func(*pbft.MessageReq)

func (t *transport) Register(name pbft.NodeID, handler transportHandler) {
	if t.nodes == nil {
		t.nodes = map[pbft.NodeID]transportHandler{}
	}
	t.nodes[name] = handler
}

func (t *transport) Gossip(msg *pbft.MessageReq) error {
	for to, handler := range t.nodes {
		go func(to pbft.NodeID, handler transportHandler) {
			send := true
			if t.hook != nil {
				send = t.hook.Gossip(msg.From, to, msg)
			}
			if send {
				// fmt.Printf("Sending message %v from Node %v to node %v\n", msg.Type, msg.From, to)
				handler(msg)
			}
		}(to, handler)
	}
	return nil
}

type transportHook interface {
	Connects(from, to pbft.NodeID) bool
	Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool
}

type msgFlow struct {
	round uint64
	// represents message flow map
	// e.g. A4 -> A0, A1
	partition map[pbft.NodeID][]pbft.NodeID
}

type roundTransport struct {
	// key is sequence
	msgSend map[uint64]msgFlow
}

func newRoundChange(flow map[uint64]msgFlow) *roundTransport {
	return &roundTransport{msgSend: flow}
}
func (rt *roundTransport) Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool {
	if msg.View.Round > 1 {
		// node A_1 is unresponsive after round 1
		if from == "A_1" || to == "A_1" {
			return false
		}
		// all other nodes are connected for all messages
		return true
	}

	// in round 1 A_1 ignores round change and commit messages
	if msg.View.Round == 1 && (msg.Type == pbft.MessageReq_RoundChange ||
		msg.Type == pbft.MessageReq_Commit) &&
		(from == "A_1") {
		return false
	}

	msgSend, ok := rt.msgSend[msg.View.Round]
	if !ok {
		return false
	}

	if msgSend.round == msg.View.Round {
		subset, ok := msgSend.partition[from]
		if !ok {
			return false
		}
		// if ok check to
		found := false
		// ignore commit messages <= of round 1
		if msg.Type == pbft.MessageReq_Commit {
			return false
		}

		for _, v := range subset {
			if v == to {
				found = true
				break
			}
		}
		return found
	}
	return true
}

func (rt *roundTransport) Connects(from, to pbft.NodeID) bool {
	return true
}

// latency transport
type randomTransport struct {
	jitterMax time.Duration
}

func newRandomTransport(jitterMax time.Duration) transportHook {
	return &randomTransport{jitterMax: jitterMax}
}

func (r *randomTransport) Connects(from, to pbft.NodeID) bool {
	return true
}

func (r *randomTransport) Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool {
	// adds random latency between the queries
	if r.jitterMax != 0 {
		tt := timeJitter(r.jitterMax)
		time.Sleep(tt)
	}
	return true
}

type partitionTransport struct {
	jitterMax time.Duration
	lock      sync.Mutex
	subsets   map[string][]string
}

func newPartitionTransport(jitterMax time.Duration) *partitionTransport {
	return &partitionTransport{jitterMax: jitterMax}
}

func (p *partitionTransport) isConnected(from, to pbft.NodeID) bool {
	subset, ok := p.subsets[string(from)]
	if !ok {
		// if not set, they are connected
		return true
	}

	found := false
	for _, i := range subset {
		if i == string(to) {
			found = true
			break
		}
	}
	return found
}

func (p *partitionTransport) Connects(from, to pbft.NodeID) bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.isConnected(from, to)
}

func (p *partitionTransport) Reset() {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.subsets = map[string][]string{}
}

func (p *partitionTransport) addSubset(from string, to []string) {
	if p.subsets == nil {
		p.subsets = map[string][]string{}
	}
	if p.subsets[from] == nil {
		p.subsets[from] = []string{}
	}
	p.subsets[from] = append(p.subsets[from], to...)
}

func (p *partitionTransport) Partition(subsets ...[]string) {
	p.lock.Lock()
	for _, subset := range subsets {
		for _, i := range subset {
			p.addSubset(i, subset)
		}
	}
	p.lock.Unlock()
}

func (p *partitionTransport) Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool {
	p.lock.Lock()
	isConnected := p.isConnected(from, to)
	p.lock.Unlock()

	if !isConnected {
		return false
	}

	time.Sleep(timeJitter(p.jitterMax))
	return true
}

func timeJitter(jitterMax time.Duration) time.Duration {
	return time.Duration(uint64(rand.Int63()) % uint64(jitterMax))
}
