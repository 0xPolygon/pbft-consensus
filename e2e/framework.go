package e2e

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"
	"go.opentelemetry.io/otel/trace"
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
	t               *testing.T
	lock            sync.Mutex
	nodes           map[string]*node
	tracer          *sdktrace.TracerProvider
	hook            transportHook
	sealedProposals []*pbft.SealedProposal
	proposer        pbft.NodeID
}

// getCurrentIndex returns a last index up to which there are inserted proposals
func (c *cluster) getCurrentIndex() uint64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	idx := 0
	if len(c.sealedProposals) > 0 {
		idx = len(c.sealedProposals) - 1
	}

	return uint64(idx)
}

// getSyncIndex returns a index up to which the node is synced with the network
func (c *cluster) getSyncIndex(node string) uint64 {
	c.nodes[node].lock.Lock()
	defer c.nodes[node].lock.Unlock()
	return c.nodes[node].syncIndex
}

// getNodeHeight returns a node height for a given index
func (c *cluster) getNodeHeight(index uint64) uint64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	height := uint64(1) // initial height is always 1 since 0 is the genesis
	if len(c.sealedProposals) != 0 {
		height = c.sealedProposals[index].Number
	}
	return height
}

// insertFinalProposal inserts final proposal from the node to the cluster and returns index up to which the node is synced
func (c *cluster) insertFinalProposal(p *pbft.SealedProposal) uint64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	for _, v := range c.sealedProposals {
		if v.Number == p.Number {
			if !v.Proposal.Equal(p.Proposal) {
				panic("Proposals are not equal")
			}
			return uint64(len(c.sealedProposals) - 1)
		}
	}
	c.sealedProposals = append(c.sealedProposals, p)
	c.proposer = p.Proposer

	return uint64(len(c.sealedProposals) - 1)
}

func (c *cluster) getCurrentHeight() uint64 {
	c.lock.Lock()
	defer c.lock.Unlock()
	height := uint64(0)
	if len(c.sealedProposals) > 0 {
		height = c.sealedProposals[len(c.sealedProposals)-1].Number
	}

	return height
}

func newPBFTCluster(t *testing.T, name, prefix string, count int, hook ...transportHook) *cluster {
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = fmt.Sprintf("%s_%d", prefix, i)
	}

	tt := &transport{}
	if len(hook) == 1 {
		tt.addHook(hook[0])
	}

	c := &cluster{
		t:               t,
		nodes:           map[string]*node{},
		tracer:          initTracer("fuzzy_" + name),
		hook:            tt.hook,
		proposer:        pbft.NodeID(""),
		sealedProposals: []*pbft.SealedProposal{},
	}
	for _, name := range names {
		trace := c.tracer.Tracer(name)
		n, _ := newPBFTNode(name, names, trace, tt)
		n.c = c
		c.nodes[name] = n
	}
	return c
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
			syncIndex := c.nodes[n].syncIndex
			height := c.getNodeHeight(syncIndex)

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

func (c *cluster) WaitForHeight(num uint64, timeout time.Duration, nodes ...[]string) error {
	// we need to check every node in the ensemble?
	// yes, this should test if everyone can agree on the final set.
	// note, if we include drops, we need to do sync otherwise this will never work

	queryNodes := c.resolveNodes(nodes...)

	enough := func() bool {
		for _, name := range queryNodes {
			syncIndex := c.getSyncIndex(name)
			if c.getNodeHeight(syncIndex) < num {
				return false
			}
		}
		return true
	}

	timer := time.NewTimer(timeout)
	for {
		select {
		case <-time.After(200 * time.Millisecond):
			if enough() {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timeout")
		}
	}
}

func (c *cluster) syncWithNetwork(ourselves string) (uint64, uint64) {
	var height uint64
	var syncIndex uint64
	for _, n := range c.nodes {
		if n.name == ourselves {
			continue
		}
		if c.hook != nil {
			// we need to see if this transport does allow those two nodes to be connected
			// Otherwise, that node should not be elegible to sync
			if !c.hook.Connects(pbft.NodeID(ourselves), pbft.NodeID(n.name)) {
				continue
			}
		}
		localHeight := n.c.getNodeHeight(n.syncIndex)
		if localHeight > height {
			height = localHeight
			syncIndex = n.syncIndex
		}
	}
	return height, syncIndex
}

func (c *cluster) lastProposer() pbft.NodeID {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.proposer
}

func (c *cluster) currentHeight() uint64 {
	c.lock.Lock()
	defer c.lock.Unlock()

	number := uint64(1) // initial height is always 1 since 0 is the genesis
	if len(c.sealedProposals) != 0 {
		number = c.sealedProposals[len(c.sealedProposals)-1].Number
	}
	return number
}

func (c *cluster) Nodes() []*node {
	list := make([]*node, len(c.nodes))
	i := 0
	for _, n := range c.nodes {
		list[i] = n
		i++
	}
	return list
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

func (c *cluster) FailNode(name string) {
	c.nodes[name].setFaultyNode(true)
}

type node struct {
	lock sync.Mutex

	c *cluster

	name     string
	pbft     *pbft.Pbft
	cancelFn context.CancelFunc
	running  uint64

	// validator nodes
	nodes []string

	// index of node synchronization with the cluster
	syncIndex uint64
	// indicate if the node is faulty
	faulty bool
}

func newPBFTNode(name string, nodes []string, trace trace.Tracer, tt *transport) (*node, error) {
	var loggerOutput io.Writer
	if os.Getenv("SILENT") == "true" {
		loggerOutput = ioutil.Discard
	} else {
		loggerOutput = os.Stdout
	}

	kk := key(name)
	con := pbft.New(kk, tt, pbft.WithTracer(trace), pbft.WithLogger(log.New(loggerOutput, "", log.LstdFlags)))

	tt.Register(pbft.NodeID(name), func(msg *pbft.MessageReq) {
		// pipe messages from mock transport to pbft
		con.PushMessage(msg)
	})

	n := &node{
		nodes:     nodes,
		name:      name,
		pbft:      con,
		syncIndex: 0,
		running:   0,
	}
	return n, nil
}

func (n *node) setSyncIndex(idx uint64) {
	n.lock.Lock()
	defer n.lock.Unlock()
	n.syncIndex = idx
}

func (n *node) isStuck(num uint64) (uint64, bool) {
	// get max height in the network
	height, _ := n.c.syncWithNetwork(n.name)

	if height > num {
		return height, true
	}
	return 0, false
}

func (n *node) Insert(pp *pbft.SealedProposal) error {
	n.lock.Lock()
	defer n.lock.Unlock()
	index := n.c.insertFinalProposal(pp)
	n.syncIndex = index
	return nil
}

func (n *node) setFaultyNode(v bool) {
	n.faulty = v
}

func (n *node) Start() {
	if n.IsRunning() {
		panic("already started")
	}

	// create the ctx and the cancelFn
	ctx, cancelFn := context.WithCancel(context.Background())
	n.cancelFn = cancelFn
	atomic.StoreUint64(&n.running, 1)
	go func() {
		defer func() {
			atomic.StoreUint64(&n.running, 0)
		}()
	SYNC:
		_, syncIndex := n.c.syncWithNetwork(n.name)
		n.setSyncIndex(syncIndex)
		for {
			fsm := &fsm{
				n:            n,
				nodes:        n.nodes,
				lastProposer: n.c.lastProposer(),

				// important: in this iteration of the fsm we have increased our height
				height:          n.c.getNodeHeight(n.syncIndex) + 1,
				validationFails: n.faulty,
			}
			if err := n.pbft.SetBackend(fsm); err != nil {
				panic(err)
			}

			// start the execution
			n.pbft.Run(ctx)

			switch n.pbft.GetState() {
			case pbft.SyncState:
				// we need to go back to sync
				goto SYNC
			case pbft.DoneState:
				// everything worked, move to the next iteration
			default:
				// stopped
				return
			}
		}
	}()
}

func (n *node) Stop() {
	if !n.IsRunning() {
		panic("already stopped")
	}
	n.cancelFn()
	// block until node is running
	for n.IsRunning() {
	}
}

func (n *node) IsRunning() bool {
	return atomic.LoadUint64(&n.running) != 0
}

func (n *node) Restart() {
	n.Stop()
	n.Start()
}

type key string

func (k key) NodeID() pbft.NodeID {
	return pbft.NodeID(k)
}

func (k key) Sign(b []byte) ([]byte, error) {
	return b, nil
}

// -- fsm --

type fsm struct {
	n               *node
	nodes           []string
	lastProposer    pbft.NodeID
	height          uint64
	validationFails bool
}

func (f *fsm) Height() uint64 {
	return f.height
}

func (f *fsm) IsStuck(num uint64) (uint64, bool) {
	return f.n.isStuck(num)
}

func (f *fsm) BuildProposal() (*pbft.Proposal, error) {
	proposal := &pbft.Proposal{
		Data: []byte{byte(f.Height())},
		Time: time.Now().Add(1 * time.Second),
	}
	proposal.Hash = hash(proposal.Data)
	return proposal, nil
}

func (f *fsm) setValidationFails(v bool) {
	f.validationFails = v
}

func (f *fsm) Validate(proposal *pbft.Proposal) error {
	if f.validationFails {
		return fmt.Errorf("validation error")
	}
	return nil
}

func (f *fsm) Insert(pp *pbft.SealedProposal) error {
	return f.n.Insert(pp)
}

func (f *fsm) ValidatorSet() pbft.ValidatorSet {
	valsAsNode := []pbft.NodeID{}
	for _, i := range f.nodes {
		valsAsNode = append(valsAsNode, pbft.NodeID(i))
	}
	vv := valString{
		nodes:        valsAsNode,
		lastProposer: f.lastProposer,
	}
	return &vv
}

func hash(p []byte) []byte {
	h := sha1.New()
	h.Write(p)
	return h.Sum(nil)
}

func (f *fsm) Init(*pbft.RoundInfo) {
}

func (f *fsm) ValidateCommit(node pbft.NodeID, seal []byte) error {
	return nil
}

type valString struct {
	nodes        []pbft.NodeID
	lastProposer pbft.NodeID
}

func (v *valString) CalcProposer(round uint64) pbft.NodeID {
	seed := uint64(0)
	if v.lastProposer == pbft.NodeID("") {
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

func (v *valString) Index(addr pbft.NodeID) int {
	for indx, i := range v.nodes {
		if i == addr {
			return indx
		}
	}
	return -1
}

func (v *valString) Includes(id pbft.NodeID) bool {
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
