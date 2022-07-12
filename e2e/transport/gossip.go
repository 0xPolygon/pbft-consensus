package transport

import "github.com/0xPolygon/pbft-consensus"

type Sender pbft.NodeID
type Receivers []pbft.NodeID

// RoundMetadata encapsulates message routing for certain round
type RoundMetadata struct {
	Round      uint64
	RoutingMap map[Sender]Receivers
}

// GossipHandler is the callback func which enables determining which message should be gossiped
type GossipHandler func(sender, receiver pbft.NodeID, msg *pbft.MessageReq) bool

// GenericGossip is the transport implementation which enables specifying custom gossiping logic
type GenericGossip struct {
	flowMap       map[uint64]RoundMetadata
	gossipHandler GossipHandler
}

// NewGenericGossip initializes new generic gossip transport
func NewGenericGossip() *GenericGossip {
	defaultGossipHandler := func(sender, receiver pbft.NodeID, msg *pbft.MessageReq) bool {
		return true
	}
	return &GenericGossip{
		flowMap:       make(map[uint64]RoundMetadata),
		gossipHandler: defaultGossipHandler,
	}
}

// WithGossipHandler attaches gossip handler
func (t *GenericGossip) WithGossipHandler(gossipHandler GossipHandler) *GenericGossip {
	t.gossipHandler = gossipHandler
	return t
}

// WithFlowMap sets message routing per round mapping
func (t *GenericGossip) WithFlowMap(flowMap map[uint64]RoundMetadata) *GenericGossip {
	t.flowMap = flowMap
	return t
}

// ShouldGossipBasedOnMsgFlowMap determines whether a message should be gossiped, based on provided flow map, which describes messages routing per round.
func (t *GenericGossip) ShouldGossipBasedOnMsgFlowMap(msg *pbft.MessageReq, senderId pbft.NodeID, receiverId pbft.NodeID) bool {
	roundMedatada, ok := t.flowMap[msg.View.Round]
	if !ok {
		return false
	}

	if roundMedatada.Round == msg.View.Round {
		receivers, ok := roundMedatada.RoutingMap[Sender(senderId)]
		if !ok {
			return false
		}

		foundReceiver := false
		for _, v := range receivers {
			if v == receiverId {
				foundReceiver = true
				break
			}
		}
		return foundReceiver
	}
	return true
}

func (t *GenericGossip) Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool {
	if t.gossipHandler != nil {
		return t.gossipHandler(from, to, msg)
	}
	return true
}

func (t *GenericGossip) Connects(from, to pbft.NodeID) bool {
	return true
}

func (t *GenericGossip) Reset() {
	t.gossipHandler = nil
}

func (t *GenericGossip) GetPartitions() map[string][]string {
	return nil
}
