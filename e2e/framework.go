package e2e

import (
	"context"
	"crypto/sha1"
	"errors"
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

type Cluster struct {
	t               *testing.T
	lock            sync.Mutex
	nodes           map[string]*node
	tracer          *sdktrace.TracerProvider
	hook            transportHook
	sealedProposals []*pbft.SealedProposal
}

func NewPBFTCluster(t *testing.T, name, prefix string, count int, hook ...transportHook) *Cluster {
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = fmt.Sprintf("%s_%d", prefix, i)
	}

	tt := &transport{}
	if len(hook) == 1 {
		tt.addHook(hook[0])
	}

	c := &Cluster{
		t:               t,
		nodes:           map[string]*node{},
		tracer:          initTracer("fuzzy_" + name),
		hook:            tt.hook,
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

// getSyncIndex returns an index up to which the node is synced with the network
func (c *Cluster) getSyncIndex(node string) int64 {
	return c.nodes[node].getSyncIndex()
}

// insertFinalProposal inserts final proposal from the node to the cluster
func (c *Cluster) insertFinalProposal(sealProp *pbft.SealedProposal) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	insertIndex := sealProp.Number - 1
	lastIndex := len(c.sealedProposals) - 1

	if lastIndex >= 0 {
		if insertIndex <= uint64(lastIndex) {
			// already exists
			if !c.sealedProposals[insertIndex].Proposal.Equal(sealProp.Proposal) {
				return errors.New("existing proposal on a given position is not not equal to the one being inserted to the same position")
			} else {
				return nil
			}
		} else if insertIndex != uint64(lastIndex+1) {
			return fmt.Errorf("expected that final proposal number is %v, but was %v", len(c.sealedProposals)+1, sealProp.Number)
		}
	}
	c.sealedProposals = append(c.sealedProposals, sealProp)
	return nil
}

func (c *Cluster) resolveNodes(nodes ...[]string) []string {
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

func (c *Cluster) IsStuck(timeout time.Duration, nodes ...[]string) {
	queryNodes := c.resolveNodes(nodes...)

	nodeHeight := map[string]uint64{}
	isStuck := func() bool {
		for _, n := range queryNodes {
			height := c.nodes[n].getNodeHeight()
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

func (c *Cluster) GetMaxHeight(nodes ...[]string) uint64 {
	queryNodes := c.resolveNodes(nodes...)
	var max uint64
	for _, node := range queryNodes {
		h, _ := c.syncWithNetwork(node)
		if h > max {
			max = h
		}
	}
	return max
}

func (c *Cluster) WaitForHeight(num uint64, timeout time.Duration, nodes ...[]string) error {
	// we need to check every node in the ensemble?
	// yes, this should test if everyone can agree on the final set.
	// note, if we include drops, we need to do sync otherwise this will never work
	queryNodes := c.resolveNodes(nodes...)

	enough := func() bool {
		c.lock.Lock()
		defer c.lock.Unlock()

		for _, name := range queryNodes {
			if c.nodes[name].getNodeHeight() < num {
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

// getNodeHeight returns node height depending on node index
// difference between height and syncIndex is 1
// first inserted proposal is on index 0 with height 1
func (n *node) getNodeHeight() uint64 {
	return uint64(n.getSyncIndex()) + 1
}

func (c *Cluster) syncWithNetwork(nodeID string) (uint64, int64) {
	c.lock.Lock()
	defer c.lock.Unlock()

	var height uint64
	var syncIndex = int64(-1) // initial sync index is -1
	for _, n := range c.nodes {
		if n.name == nodeID {
			continue
		}
		if c.hook != nil {
			// we need to see if this transport does allow those two nodes to be connected
			// Otherwise, that node should not be eligible to sync
			if !c.hook.Connects(pbft.NodeID(nodeID), pbft.NodeID(n.name)) {
				continue
			}
		}
		localHeight := n.getNodeHeight()
		if localHeight > height {
			height = localHeight
			syncIndex = int64(localHeight) - 1 // we know that syncIndex is less than height by 1
		}
	}
	return height, syncIndex
}

func (c *Cluster) getProposer(index int64) pbft.NodeID {
	c.lock.Lock()
	defer c.lock.Unlock()

	proposer := pbft.NodeID("")
	if index >= 0 && int64(len(c.sealedProposals)-1) >= index {
		proposer = c.sealedProposals[index].Proposer
	}

	return proposer
}

func (n *node) currentHeight() uint64 {
	height := uint64(0) // initial height is always 0
	index := n.getSyncIndex()
	if index >= 0 {
		height = uint64(index) + 1
	}
	return height
}

func (c *Cluster) Nodes() []*node {
	list := make([]*node, len(c.nodes))
	i := 0
	for _, n := range c.nodes {
		list[i] = n
		i++
	}
	return list
}

func (c *Cluster) GetFilteredNodes(filter func(*node) bool) (filteredNodes []*node) {
	for _, n := range c.nodes {
		if filter(n) {
			filteredNodes = append(filteredNodes, n)
		}
	}
	return
}

func (c *Cluster) GetRunningNodes() []*node {
	return c.GetFilteredNodes(func(n *node) bool {
		return n.IsRunning()
	})
}

func (c *Cluster) GetStoppedNodes() []*node {
	return c.GetFilteredNodes(func(n *node) bool {
		return !n.IsRunning()
	})
}

func (c *Cluster) Start() {
	for _, n := range c.nodes {
		n.Start()
	}
}

func (c *Cluster) StartNode(name string) {
	c.nodes[name].Start()
}

func (c *Cluster) StopNode(name string) {
	c.nodes[name].Stop()
}

func (c *Cluster) Stop() {
	for _, n := range c.nodes {
		n.Stop()
	}
	if err := c.tracer.Shutdown(context.Background()); err != nil {
		panic("failed to shutdown TracerProvider")
	}
}

type node struct {
	// index of node synchronization with the cluster
	localSyncIndex int64

	c *Cluster

	name     string
	pbft     *pbft.Pbft
	cancelFn context.CancelFunc
	running  uint64

	// validator nodes
	nodes []string

	// indicate if the node is faulty
	faulty uint64
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
		nodes:   nodes,
		name:    name,
		pbft:    con,
		running: 0,
		// set to init index -1 so that zero value is not the same as first index
		localSyncIndex: -1,
	}
	return n, nil
}

func (n *node) getSyncIndex() int64 {
	return atomic.LoadInt64(&n.localSyncIndex)
}

func (n *node) setSyncIndex(idx int64) {
	atomic.StoreInt64(&n.localSyncIndex, idx)
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
	err := n.c.insertFinalProposal(pp)
	if err != nil {
		panic(err)
	}
	return nil
}

// setFaultyNode sets flag indicating that the node should be faulty or not
// 0 is for not being faulty
func (n *node) setFaultyNode(b bool) {
	if b {
		atomic.StoreUint64(&n.faulty, 1)
	} else {
		atomic.StoreUint64(&n.faulty, 0)
	}
}

// isFaulty checks if the node should be faulty or not depending on the stored value
// 0 is for not being faulty
func (n *node) isFaulty() bool {
	return atomic.LoadUint64(&n.faulty) != 0
}

func (n *node) Start() {
	if n.IsRunning() {
		panic(fmt.Errorf("node '%s' is already started", n))
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
				lastProposer: n.c.getProposer(n.getSyncIndex()),

				// important: in this iteration of the fsm we have increased our height
				height:          n.getNodeHeight() + 1,
				validationFails: n.isFaulty(),
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
				currentSyncIndex := n.getSyncIndex()
				n.setSyncIndex(currentSyncIndex + 1)
			default:
				// stopped
				return
			}
		}
	}()
}

func (n *node) Stop() {
	if !n.IsRunning() {
		panic(fmt.Errorf("node %s is already stopped", n.name))
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

func (n *node) GetName() string {
	return n.name
}

func (n *node) String() string {
	return n.name
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

func newSealedProposal(proposalData []byte, proposer pbft.NodeID, number uint64) *pbft.SealedProposal {
	proposal := &pbft.Proposal{
		Data: proposalData,
		Time: time.Now(),
	}
	proposal.Hash = hash(proposal.Data)
	return &pbft.SealedProposal{
		Proposal: proposal,
		Proposer: proposer,
		Number:   number,
	}
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
