package e2e

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e/notifier"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.7.0"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e/helper"
	"github.com/0xPolygon/pbft-consensus/e2e/transport"
)

var (
	errMaxFaultyEmptyCluster  = errors.New("unable to determine max faulty nodes: cluster is empty")
	errQuorumSizeEmptyCluster = errors.New("unable to determine quorum size, because cluster is empty")
)

// CreateBackend is a delegate that creates a new instance of IntegrationBackend interface
type CreateBackend func() IntegrationBackend

type ClusterConfig struct {
	Count                 int
	Name                  string
	Prefix                string
	LogsDir               string
	ReplayMessageNotifier notifier.Notifier
	TransportHandler      transport.Handler
	RoundTimeout          pbft.RoundTimeout
	CreateBackend         CreateBackend
}

type Cluster struct {
	t                     *testing.T
	lock                  sync.Mutex
	nodes                 map[string]*node
	tracer                *sdktrace.TracerProvider
	transport             *transport.Transport
	sealedProposals       []*pbft.SealedProposal
	replayMessageNotifier notifier.Notifier
	createBackend         CreateBackend
}

// NewPBFTCluster is the constructor of Cluster
func NewPBFTCluster(t *testing.T, config *ClusterConfig, hook ...transport.Hook) *Cluster {
	names := make([]string, config.Count)
	for i := 0; i < config.Count; i++ {
		names[i] = fmt.Sprintf("%s_%d", config.Prefix, i)
	}

	tt := &transport.Transport{}
	if len(hook) == 1 {
		tt.AddHook(hook[0])
	}

	var directoryName string
	if t != nil {
		directoryName = t.Name()
	}

	if config.ReplayMessageNotifier == nil {
		config.ReplayMessageNotifier = &notifier.DefaultNotifier{}
	}

	if config.CreateBackend == nil {
		config.CreateBackend = func() IntegrationBackend { return &BackendFake{} }
	}

	logsDir, err := helper.CreateLogsDir(directoryName)
	if err != nil {
		log.Printf("[WARNING] Could not create logs directory. Reason: %v. Logging will be defaulted to standard output.", err)
	} else {
		config.LogsDir = logsDir
	}

	tt.SetLogger(log.New(helper.GetLoggerOutput("transport", logsDir), "", log.LstdFlags))

	c := &Cluster{
		t:                     t,
		nodes:                 map[string]*node{},
		tracer:                initTracer("fuzzy_" + config.Name),
		transport:             tt,
		sealedProposals:       []*pbft.SealedProposal{},
		replayMessageNotifier: config.ReplayMessageNotifier,
		createBackend:         config.CreateBackend,
	}

	err = c.replayMessageNotifier.SaveMetaData(&names)
	if err != nil {
		log.Printf("[WARNING] Could not write node meta data to replay messages file. Reason: %v", err)
	}

	for _, name := range names {
		trace := c.tracer.Tracer(name)
		n, _ := newPBFTNode(name, config, names, trace, tt)
		n.c = c
		c.nodes[name] = n
	}
	return c
}

func (c *Cluster) Nodes() []*node {
	c.lock.Lock()
	defer c.lock.Unlock()

	list := make([]*node, len(c.nodes))
	i := 0
	for _, n := range c.nodes {
		list[i] = n
		i++
	}
	return list
}

func (c *Cluster) SetHook(hook transport.Hook) {
	c.transport.AddHook(hook)
}

func (c *Cluster) GetMaxHeight(nodes ...[]string) uint64 {
	queryNodes := c.resolveNodes(nodes...)
	var max uint64
	for _, node := range queryNodes {
		h := c.nodes[node].GetNodeHeight()
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
			if c.nodes[name].GetNodeHeight() < num {
				return false
			}
		}
		return true
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-time.After(300 * time.Millisecond):
			if enough() {
				return nil
			}
		case <-timer.C:
			return fmt.Errorf("timeout")
		}
	}
}

func (c *Cluster) GetNodesMap() map[string]*node {
	return c.nodes
}

func (c *Cluster) GetRunningNodes() []*node {
	return c.getFilteredNodes(func(n *node) bool {
		return n.IsRunning()
	})
}

func (c *Cluster) Start() {
	for _, n := range c.nodes {
		n.Start()
	}
}

func (c *Cluster) StopNode(name string) {
	c.nodes[name].Stop()
}

func (c *Cluster) Stop() {
	for _, n := range c.nodes {
		if n.IsRunning() {
			n.Stop()
		}
	}
	if err := c.tracer.Shutdown(context.Background()); err != nil {
		panic("failed to shutdown TracerProvider")
	}
}

func (c *Cluster) GetTransportHook() transport.Hook {
	return c.transport.GetHook()
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
			}

			return nil
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

func (c *Cluster) isStuck(timeout time.Duration, nodes ...[]string) {
	queryNodes := c.resolveNodes(nodes...)

	nodeHeight := map[string]uint64{}
	isStuck := func() bool {
		for _, n := range queryNodes {
			height := c.nodes[n].GetNodeHeight()
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

func (c *Cluster) getNodes() []*node {
	nodes := make([]*node, 0, len(c.nodes))
	for _, nd := range c.nodes {
		nodes = append(nodes, nd)
	}
	return nodes
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
		if hook := c.transport.GetHook(); hook != nil {
			// we need to see if this transport does allow those two nodes to be connected
			// Otherwise, that node should not be eligible to sync
			if !hook.Connects(pbft.NodeID(nodeID), pbft.NodeID(n.name)) {
				continue
			}
		}
		localHeight := n.GetNodeHeight()
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

// getFilteredNodes returns nodes which satisfy provided filter delegate function.
// If filter is not provided, all the nodes will be retreived.
func (c *Cluster) getFilteredNodes(filter func(*node) bool) []*node {
	if filter != nil {
		var filteredNodes []*node
		for _, n := range c.nodes {
			if filter(n) {
				filteredNodes = append(filteredNodes, n)
			}
		}
		return filteredNodes
	}
	return c.getNodes()
}

func (c *Cluster) startNode(name string) {
	c.nodes[name].Start()
}

// MaxFaulty is a wrapper function which invokes MaxFaultyVotingPower on PBFT consensus instance of the first node in cluster
func (c *Cluster) MaxFaulty() (uint64, error) {
	nodes := c.getNodes()
	if len(nodes) == 0 {
		return 0, errMaxFaultyEmptyCluster
	}
	return nodes[0].pbft.MaxFaultyVotingPower(), nil
}

// QuorumSize is a wrapper function which invokes QuorumSize on PBFT consensus instance of the first node in cluster
func (c *Cluster) QuorumSize() (uint64, error) {
	nodes := c.getNodes()
	if len(nodes) == 0 {
		return 0, errQuorumSizeEmptyCluster
	}
	return nodes[0].pbft.QuorumSize(), nil
}

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
