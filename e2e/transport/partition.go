package transport

import (
	"sync"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

type Partition struct {
	jitterMax time.Duration
	lock      sync.Mutex
	subsets   map[string][]string
}

func NewPartition(jitterMax time.Duration) *Partition {
	return &Partition{jitterMax: jitterMax}
}

func (p *Partition) isConnected(from, to pbft.NodeID) bool {
	if p.subsets == nil {
		return true
	}

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

func (p *Partition) Connects(from, to pbft.NodeID) bool {
	p.lock.Lock()
	defer p.lock.Unlock()

	return p.isConnected(from, to)
}

func (p *Partition) Reset() {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.subsets = nil
}

func (p *Partition) GetPartitions() map[string][]string {
	return p.subsets
}

func (p *Partition) addSubset(from string, to []string) {
	if p.subsets == nil {
		p.subsets = map[string][]string{}
	}
	if p.subsets[from] == nil {
		p.subsets[from] = []string{}
	}
	p.subsets[from] = append(p.subsets[from], to...)
}

func (p *Partition) Partition(subsets ...[]string) {
	p.lock.Lock()
	for _, subset := range subsets {
		for _, i := range subset {
			p.addSubset(i, subset)
		}
	}
	p.lock.Unlock()
}

func (p *Partition) Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool {
	p.lock.Lock()
	isConnected := p.isConnected(from, to)
	p.lock.Unlock()

	if !isConnected {
		return false
	}

	time.Sleep(timeJitter(p.jitterMax))
	return true
}
