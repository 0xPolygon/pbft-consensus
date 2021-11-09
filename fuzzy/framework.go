package fuzzy

import (
	"context"
	"fmt"
	"testing"

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
	// otel.SetTracerProvider(tracerProvider)

	// set global propagator to tracecontext (the default is no-op).
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tracerProvider
}

type cluster struct {
	nodes  []*node
	tracer *sdktrace.TracerProvider
}

func newIBFTCluster(t *testing.T, prefix string, count int) *cluster {
	names := make([]string, count)
	for i := 0; i < count; i++ {
		names[i] = fmt.Sprintf("%s_%d", prefix, i)
	}

	tt := &transport{}

	c := &cluster{
		nodes:  []*node{},
		tracer: initTracer("fuzzy_" + prefix),
	}
	for _, name := range names {
		trace := c.tracer.Tracer(name)
		n, err := newIBFTNode(name, names, trace, tt)
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
	if err := c.tracer.Shutdown(context.Background()); err != nil {
		panic("failed to shutdown TracerProvider")
	}
}

type node struct {
	name    string
	fsm     *fsm
	ibft    *ibft.Ibft
	closeCh chan struct{}
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
