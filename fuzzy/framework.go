package fuzzy

import (
	"context"
	"fmt"
	"testing"

	"github.com/0xPolygon/ibft-consensus"
)

type cluster struct {
	nodes []*node
}

func newIBFTCluster(t *testing.T, prefix string, count int) *cluster {
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = fmt.Sprintf("%s_%d", prefix, i)
	}

	tt := &transport{}

	c := &cluster{
		nodes: []*node{},
	}
	for _, name := range names {
		n, err := newIBFTNode(name, names, tt)
		if err != nil {
			t.Fatal(err)
		}
		c.nodes = append(c.nodes, n)
	}
	return c
}

func (c *cluster) Start() {
	for _, n := range c.nodes {
		n.Run()
	}
}

func (c *cluster) Stop() {
	for _, n := range c.nodes {
		n.Stop()
	}
}

type node struct {
	name    string
	fsm     *fsm
	ibft    *ibft.Ibft
	closeCh chan struct{}
}

func newIBFTNode(name string, nodes []string, tt *transport) (*node, error) {
	kk := key(name)
	fsm := &fsm{
		nodes:  nodes,
		number: 1, // next sequence
	}

	con, _ := ibft.Factory(nil, fsm, kk, tt)

	tt.Register(name, func(msg *ibft.MessageReq) {
		// pipe messages from mock transport to ibft
		con.PushMessage(msg)
	})

	n := &node{
		name:    name,
		ibft:    con,
		fsm:     fsm,
		closeCh: make(chan struct{}),
	}
	return n, nil
}

func (n *node) Run() {
	// since we already know we are synced

	// this mocks the sync protocl we have to start the view with the initial block
	// that we just synced up with
	n.ibft.SetSequence(0)

	go func() {
		n.ibft.Run(context.Background())
		fmt.Println("- sync done -")
	}()
}

func (n *node) Stop() {
	// TODO
}

type key string

func (k key) NodeID() ibft.NodeID {
	return ibft.NodeID(k)
}

func (k key) Sign(b []byte) ([]byte, error) {
	return b, nil
}

type transport struct {
	nodes map[string]func(*ibft.MessageReq)
}

func (t *transport) Register(name string, handler func(*ibft.MessageReq)) {
	if t.nodes == nil {
		t.nodes = map[string]func(*ibft.MessageReq){}
	}
	t.nodes[name] = handler
}

func (t *transport) Gossip(msg *ibft.MessageReq) error {
	for _, handler := range t.nodes {
		handler(msg)
	}
	return nil
}
