package e2e

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/0xPolygon/ibft-consensus"
)

type transport struct {
	nodes map[ibft.NodeID]transportHandler
	hook  transportHook
}

func (t *transport) addHook(hook transportHook) {
	t.hook = hook
}

type transportHandler func(*ibft.MessageReq)

func (t *transport) Register(name ibft.NodeID, handler transportHandler) {
	if t.nodes == nil {
		t.nodes = map[ibft.NodeID]transportHandler{}
	}
	t.nodes[name] = handler
}

func (t *transport) Gossip(msg *ibft.MessageReq) error {
	for to, handler := range t.nodes {
		go func(to ibft.NodeID, handler transportHandler) {
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
	Connects(from, to ibft.NodeID) bool
	Gossip(from, to ibft.NodeID, msg *ibft.MessageReq) bool
}

// latency transport
type randomTransport struct {
	jitterMax time.Duration
}

func newRandomTransport(jitterMax time.Duration) transportHook {
	return &randomTransport{jitterMax: jitterMax}
}

func (r *randomTransport) Connects(from, to ibft.NodeID) bool {
	return true
}

func (r *randomTransport) Gossip(from, to ibft.NodeID, msg *ibft.MessageReq) bool {
	// adds random latency between the queries
	if r.jitterMax != 0 {
		tt := timeJitter(r.jitterMax)
		fmt.Println("- sleep ", tt)

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

func (p *partitionTransport) isConnected(from, to ibft.NodeID) bool {
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

func (p *partitionTransport) Connects(from, to ibft.NodeID) bool {
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

func (p *partitionTransport) Gossip(from, to ibft.NodeID, msg *ibft.MessageReq) bool {
	// var duration time.Duration

	p.lock.Lock()
	isConnected := p.isConnected(from, to)
	p.lock.Unlock()

	if !isConnected {
		return false
	}

	time.Sleep(timeJitter(p.jitterMax))
	return true

	/*
		if subset, ok := p.subsets[string(from)]; ok {
			found := false
			for _, i := range subset {
				if i == string(to) {
					found = true
					break
				}
			}
			fmt.Println(from, to, found)

			// if not found drop the message
			if found {
				// add normal jitter
				duration = timeJitter(p.jitterMax)
			}
		} else {
			// if nothing subset yet, assume a healthy network
			duration = timeJitter(p.jitterMax)
		}

		// make sure we unlock before sleeping
		p.lock.Unlock()

		if duration == 0 {
			return false
		}

		time.Sleep(duration)
		return true
	*/
}

/*
type daemon struct {
	r *rand.Rand
	c *cluster
}

func (d *daemon) Start() {
	go d.start()
}

func (d *daemon) failProb(ratio float64) bool {
	return d.r.Float64() < ratio
}

func randomInt(min, max int) int {
	return min + rand.Intn(max-min)
}

func (d *daemon) dropPeer() {

}

func (d *daemon) start() {
	scenarios := []func(){
		d.dropPeer,
	}

	for {
		// TODO: close it
		<-time.After(1 * time.Second)

		if d.failProb(0.10) {
			// 10% change of messing with the ensemble
			indx := randomInt(0, len(scenarios))
			scenarios[indx]()
		}
	}
}
*/

func timeJitter(jitterMax time.Duration) time.Duration {
	return time.Duration(uint64(rand.Int63()) % uint64(jitterMax))
}
