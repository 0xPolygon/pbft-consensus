package fuzzy

import (
	"context"
	"fmt"
	"math/rand"
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

func (c *cluster) WaitForHeight(num uint64, timeout time.Duration) {
	// we need to check every node in the ensemble?
	// yes, this should test if everyone can agree on the final set.
	// note, if we include drops, we need to do sync otherwise this will never work

	enough := func() bool {
		for _, n := range c.nodes {
			if n.fsm.currentHeight() < num {
				return false
			}
		}
		return true
	}

	for {
		select {
		case <-time.After(100 * time.Millisecond):
			if enough() {
				return
			}
		case <-time.After(timeout):
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
			// this is the best idea so far to mock async messages
			if t.hook != nil {
				t.hook.Gossip(msg.From, to, msg)
			}
			handler(msg)
		}(to, handler)
	}
	return nil
}

type transportHook interface {
	Gossip(from, to ibft.NodeID, msg *ibft.MessageReq)
}

// latency transport
type randomTransport struct {
	jitterMax time.Duration
}

func newRandomTransport(jitterMax time.Duration) transportHook {
	return &randomTransport{jitterMax: jitterMax}
}

func (r *randomTransport) Gossip(from, to ibft.NodeID, msg *ibft.MessageReq) {
	// adds random latency between the queries
	if r.jitterMax != 0 {
		tt := timeJitter(r.jitterMax)
		fmt.Println("- sleep ", tt)

		time.Sleep(tt)
	}
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
