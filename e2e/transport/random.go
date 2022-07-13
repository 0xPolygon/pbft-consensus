package transport

import (
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

// random is the latency transport
type random struct {
	jitterMax time.Duration
}

func NewRandom(jitterMax time.Duration) Hook {
	return &random{jitterMax: jitterMax}
}

func (r *random) Connects(from, to pbft.NodeID) bool {
	return true
}

func (r *random) Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool {
	// adds random latency between the queries
	if r.jitterMax != 0 {
		tt := timeJitter(r.jitterMax)
		time.Sleep(tt)
	}
	return true
}

func (r *random) Reset() {
	// no impl
}

func (r *random) GetPartitions() map[string][]string {
	return nil
}
