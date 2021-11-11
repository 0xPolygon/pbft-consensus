package e2e

import (
	"context"
	"crypto/sha1"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/ibft-consensus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
)

func initTracer(name string) *sdktrace.TracerProvider {
	ctx := context.Background()

	res, err := resource.New(ctx,
		resource.WithAttributes(
			// the service name used to display traces in backends
			semconv.ServiceNameKey.String(name),
		),
	)
	if err != nil {
		panic("failed to create resource")
	}

	// Set up a trace exporter
	traceExporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(),
		otlptracegrpc.WithEndpoint("localhost:4317"),
		otlptracegrpc.WithDialOption(grpc.WithBlock()),
	)
	if err != nil {
		panic("failed to trace exporter")
	}

	// Register the trace exporter with a TracerProvider, using a batch
	// span processor to aggregate spans before export.
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)

	// set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tracerProvider
}

type cluster struct {
	t      *testing.T
	nodes  map[string]*node
	tracer *sdktrace.TracerProvider
	hook   transportHook
}

func newIBFTCluster(t *testing.T, prefix string, count int, hook ...transportHook) *cluster {
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = fmt.Sprintf("%s_%d", prefix, i)
	}

	tt := &transport{}
	if len(hook) == 1 {
		tt.addHook(hook[0])
	}

	c := &cluster{
		t:      t,
		nodes:  map[string]*node{},
		tracer: initTracer("fuzzy_" + prefix),
		hook:   tt.hook,
	}
	for _, name := range names {
		trace := c.tracer.Tracer(name)
		n, _ := newIBFTNode(name, names, trace, tt)
		n.c = c
		c.nodes[name] = n
	}
	return c
}

func (c *cluster) syncWithNetwork(ourselves string) (uint64, []*ibft.Proposal2) {
	var height uint64
	var proposals []*ibft.Proposal2

	for _, n := range c.nodes {
		if n.name == ourselves {
			continue
		}
		if c.hook != nil {
			// we need to see if this transport does allow those two nodes to be connected
			// Otherwise, that node should not be elegible to sync
			if !c.hook.Connects(ibft.NodeID(ourselves), ibft.NodeID(n.name)) {
				continue
			}
		}
		localHeight, data := n.fsm.getProposals()
		if localHeight > height {
			height = localHeight
			proposals = data
		}
	}
	return height, proposals
}

func (c *cluster) printHeight() {
	fmt.Println("- height -")

	for _, n := range c.nodes {
		fmt.Println(n.fsm.currentHeight())
	}
}

func (c *cluster) resolveNodes(nodes ...[]string) []string {
	queryNodes := []string{}
	if len(nodes) == 1 {
		for _, n := range nodes[0] {
			if _, ok := c.nodes[n]; !ok {
				panic("node not found in query")
			}
		}
		queryNodes = nodes[0]
	} else {
		for n := range c.nodes {
			queryNodes = append(queryNodes, n)
		}
	}
	return queryNodes
}

func (c *cluster) IsStuck(timeout time.Duration, nodes ...[]string) {
	queryNodes := c.resolveNodes(nodes...)

	nodeHeight := map[string]uint64{}
	isStuck := func() bool {
		for _, n := range queryNodes {
			height := c.nodes[n].fsm.currentHeight()
			if lastHeight, ok := nodeHeight[n]; ok {
				if lastHeight != height {
					return false
				}
			} else {
				nodeHeight[n] = height
			}
		}
		return true
	}

	timer := time.NewTimer(timeout)
	for {
		select {
		case <-time.After(200 * time.Millisecond):
			if !isStuck() {
				c.t.Fatal("it is not stuck")
			}
		case <-timer.C:
			return
		}
	}
}

func (c *cluster) WaitForHeight(num uint64, timeout time.Duration, nodes ...[]string) {
	// we need to check every node in the ensemble?
	// yes, this should test if everyone can agree on the final set.
	// note, if we include drops, we need to do sync otherwise this will never work

	queryNodes := c.resolveNodes(nodes...)

	enough := func() bool {
		for _, name := range queryNodes {
			if c.nodes[name].fsm.currentHeight() < num {
				return false
			}
		}
		return true
	}

	timer := time.NewTimer(timeout)
	for {
		select {
		case <-time.After(100 * time.Millisecond):
			if enough() {
				return
			}
		case <-timer.C:
			c.t.Fatal("timeout")
		}
	}
}

func (c *cluster) Start() {
	for _, n := range c.nodes {
		n.Start()
	}
}

func (c *cluster) StartNode(name string) {
	c.nodes[name].Start()
}

func (c *cluster) StopNode(name string) {
	c.nodes[name].Stop()
}

func (c *cluster) Stop() {
	for _, n := range c.nodes {
		n.Stop()
	}
	if err := c.tracer.Shutdown(context.Background()); err != nil {
		panic("failed to shutdown TracerProvider")
	}
}

type node struct {
	c *cluster

	name     string
	fsm      *fsm
	ibft     *ibft.Ibft
	cancelFn context.CancelFunc
	stopped  uint64
}

func newIBFTNode(name string, nodes []string, trace trace.Tracer, tt *transport) (*node, error) {
	kk := key(name)
	fsm := &fsm{
		nodes: nodes,
		// number:    1, // next sequence
		proposals: []*ibft.Proposal2{},
	}

	con, _ := ibft.Factory(nil, fsm, kk, tt)
	con.SetTrace(trace)

	tt.Register(ibft.NodeID(name), func(msg *ibft.MessageReq) {
		// pipe messages from mock transport to ibft
		con.PushMessage(msg)
	})

	n := &node{
		name:    name,
		ibft:    con,
		fsm:     fsm,
		stopped: 0,
	}
	fsm.n = n
	return n, nil
}

func (n *node) isStuck(num uint64) (uint64, bool) {

	// get max heigh in the network
	height, _ := n.c.syncWithNetwork(n.name)

	if height > num {
		return height, true
	}
	return 0, false
}

/*
func (n *node) Run() {
	// since we already know we are synced

	fmt.Println("-- initial sync --")
	fmt.Println(n.c.syncWithNetwork(n.name))

	// this mocks the sync protocl we have to start the view with the initial block
	// that we just synced up with
	n.ibft.SetSequence(0)

	ctx, cancelFn := context.WithCancel(context.Background())
	go func() {
		<-n.closeCh
		cancelFn()
	}()

	// we need to do more stuff here.

	go func() {
		n.ibft.Run(ctx)
		fmt.Println("- sync done -")

		time.Sleep(5 * time.Second)

		fmt.Println("-- sync --")
		fmt.Println(n.c.syncWithNetwork(n.name))

		panic("X")
		// panic("done??")
	}()
}
*/

func (n *node) Start() {
	if n.cancelFn != nil {
		panic("already started")
	}

	// create the ctx and the cancelFn
	ctx, cancelFn := context.WithCancel(context.Background())
	n.cancelFn = cancelFn

	go func() {
	SYNC:
		// try to sync with the network
		_, history := n.c.syncWithNetwork(n.name)
		n.fsm.proposals = history

		// reset the ibft protocol, I think this is already done

		// set the current target height for the ibft
		n.ibft.SetSequence(n.fsm.currentHeight() + 1)

		// start the execution
		n.ibft.Run(ctx)

		// ibft stopped, it can either be a node stop (from cancel context)
		// or it is stucked
		if n.ibft.GetState() == ibft.SyncState {
			goto SYNC
		}
	}()
}

func (n *node) IsRunning() bool {
	return n.cancelFn != nil
}

func (n *node) Stop() {
	if n.cancelFn == nil {
		panic("already stopped")
	}
	n.cancelFn()
	n.cancelFn = nil
}

type key string

func (k key) NodeID() ibft.NodeID {
	return ibft.NodeID(k)
}

func (k key) Sign(b []byte) ([]byte, error) {
	return b, nil
}

// -- fsm --

type fsm struct {
	n         *node
	nodes     []string
	proposals []*ibft.Proposal2
	lock      sync.Mutex
}

func (f *fsm) getProposals() (uint64, []*ibft.Proposal2) {
	f.lock.Lock()
	defer f.lock.Unlock()

	res := []*ibft.Proposal2{}
	for _, r := range f.proposals {
		res = append(res, r)
	}
	number := uint64(0)
	if len(res) != 0 {
		number = uint64(res[len(res)-1].Number)
	}
	return number, res
}

func (f *fsm) currentHeight() uint64 {
	f.lock.Lock()
	defer f.lock.Unlock()

	number := uint64(1) // initial height is always 1 since 0 is the genesis
	if len(f.proposals) != 0 {
		number = f.proposals[len(f.proposals)-1].Number
	}
	return number
}

func (f *fsm) nextHeight() uint64 {
	return f.currentHeight() + 1
}

func (f *fsm) IsStuck(num uint64) (uint64, bool) {
	return f.n.isStuck(num)
}

func (f *fsm) BuildBlock() (*ibft.Proposal, error) {
	proposal := &ibft.Proposal{
		Data: []byte{byte(f.nextHeight())},
		Time: time.Now().Add(1 * time.Second),
	}
	return proposal, nil
}

func (f *fsm) Validate(proposal []byte) ([]byte, error) {
	// always validate for now
	return nil, nil
}

func (f *fsm) Insert(pp *ibft.Proposal2) error {
	f.lock.Lock()
	defer f.lock.Unlock()

	f.proposals = append(f.proposals, pp)
	return nil
}

func (f *fsm) ValidatorSet() (*ibft.Snapshot, error) {
	valsAsNode := []ibft.NodeID{}
	for _, i := range f.nodes {
		valsAsNode = append(valsAsNode, ibft.NodeID(i))
	}
	vv := valString{
		nodes: valsAsNode,
	}
	// set the last proposer if any
	if len(f.proposals) != 0 {
		vv.lastProposer = f.proposals[len(f.proposals)-1].Proposer
	}

	// get the current number from last proposal if any (otherwise 0)
	snap := &ibft.Snapshot{
		ValidatorSet: &vv,
		Number:       f.nextHeight(),
	}
	return snap, nil
}

func (f *fsm) Hash(p []byte) ([]byte, error) {
	h := sha1.New()
	h.Write(p)
	return h.Sum(nil), nil
}

type valString struct {
	nodes        []ibft.NodeID
	lastProposer ibft.NodeID
}

func (v *valString) CalcProposer(round uint64) ibft.NodeID {
	seed := uint64(0)
	if v.lastProposer == ibft.NodeID("") {
		seed = round
	} else {
		offset := 0
		if indx := v.Index(v.lastProposer); indx != -1 {
			offset = indx
		}
		seed = uint64(offset) + round + 1
	}

	pick := seed % uint64(v.Len())
	return (v.nodes)[pick]
}

func (v *valString) Index(addr ibft.NodeID) int {
	for indx, i := range v.nodes {
		if i == addr {
			return indx
		}
	}
	return -1
}

func (v *valString) Includes(id ibft.NodeID) bool {
	for _, i := range v.nodes {
		if i == id {
			return true
		}
	}
	return false
}

func (v *valString) Len() int {
	return len(v.nodes)
}
