package transport

import (
	"log"
	"sync"

	"github.com/0xPolygon/pbft-consensus"
)

type Hook interface {
	Connects(from, to pbft.NodeID) bool
	Gossip(from, to pbft.NodeID, msg *pbft.MessageReq) bool
	Reset()
	GetPartitions() map[string][]string
}

type Handler func(pbft.NodeID, *pbft.MessageReq)

type Transport struct {
	lock   sync.Mutex
	logger *log.Logger
	nodes  map[pbft.NodeID]Handler
	hook   Hook
}

func (t *Transport) SetLogger(logger *log.Logger) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.logger = logger
}

func (t *Transport) AddHook(hook Hook) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.hook = hook
}

func (t *Transport) GetHook() Hook {
	t.lock.Lock()
	defer t.lock.Unlock()
	return t.hook
}

func (t *Transport) Register(name pbft.NodeID, handler Handler) {
	if t.nodes == nil {
		t.nodes = map[pbft.NodeID]Handler{}
	}
	t.nodes[name] = handler
}

func (t *Transport) Gossip(msg *pbft.MessageReq) error {
	for to, handler := range t.nodes {
		if msg.From == to {
			continue
		}
		go func(to pbft.NodeID, handler Handler) {
			send := true
			if hook := t.GetHook(); hook != nil {
				send = hook.Gossip(msg.From, to, msg)
			}
			if send {
				handler(to, msg)
				t.logger.Printf("[TRACE] Message sent to %s - %s", to, msg)
			} else {
				t.logger.Printf("[TRACE] Message not sent to %s - %s", to, msg)
			}
		}(to, handler)
	}
	return nil
}
