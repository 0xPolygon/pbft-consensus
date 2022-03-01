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
				handler(msg)
			}
		}(to, handler)
	}
	return nil
}

type transportHook interface {
	Connects(from, to pbft.NodeID) bool
	Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool
	Reset()
	GetPartitions() map[string][]string
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
func (r *randomTransport) Reset() {
	// no impl
}

func (r *randomTransport) GetPartitions() map[string][]string {
	return nil
}

type partitionTransport struct {
	jitterMax time.Duration
	lock      sync.Mutex
	subsets   map[string][]string
}

type flowMapTransport struct {
	lock    sync.Mutex
	flowMap map[string][]string
}

func newFlowMapTransport() *flowMapTransport {
	return &flowMapTransport{}
}

func (f *flowMapTransport) isConnected(from, to pbft.NodeID) bool {
	if nodes, ok := f.flowMap[string(from)]; ok {
		return Contains(nodes, string(to))
	} else {
		return false
	}
}

func (f *flowMapTransport) Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool {
	f.lock.Lock()
	defer f.lock.Unlock()
	return f.isConnected(from, to)
}

func (f *flowMapTransport) Connects(from, to pbft.NodeID) bool {
	f.lock.Lock()
	defer f.lock.Unlock()

	return f.isConnected(from, to)
}

func (f *flowMapTransport) Reset() {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.flowMap = map[string][]string{}
}

func (f *flowMapTransport) GetPartitions() map[string][]string {
	return f.flowMap
}

func (f *flowMapTransport) SetFlowMap(flowMap map[string][]string) {
	f.flowMap = flowMap
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

func (p *partitionTransport) GetPartitions() map[string][]string {
	return p.subsets
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
