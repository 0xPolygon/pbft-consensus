package e2e

import (
	"context"
	"fmt"
	"github.com/0xPolygon/pbft-consensus"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/goleak"
	"golang.org/x/sync/errgroup"
	"io"
	"log"
	"pgregory.net/rapid"
	"strconv"
	"sync"
	"testing"
	"time"
)

type fakeTransport struct {
	nodes      []*pbft.Pbft
	GossipFunc func(ft *fakeTransport, msg *pbft.MessageReq) error
}

func (ft *fakeTransport) Gossip(msg *pbft.MessageReq) error {
	if ft.GossipFunc != nil {
		return ft.GossipFunc(ft, msg)
	}

	for _, node := range ft.nodes {
		if msg.From != node.GetValidatorId() {
			node.PushMessage(msg.Copy())
		}
	}
	return nil
}
func TestName1(t *testing.T) {
	defer goleak.VerifyNone(t)

	ft := &fakeTransport{}

	nodes := []string{}
	createNode := func(name string) *pbft.Pbft {
		node := pbft.New(key(name), ft, pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")), func(config *pbft.Config) {
		})
		nodes = append(nodes, name)
		ft.nodes = append(ft.nodes, node)
		return node
	}
	numOfNodes := 10
	cluster := make([]*pbft.Pbft, numOfNodes)
	for i := 0; i < numOfNodes; i++ {
		cluster[i] = createNode(strconv.Itoa(i))
	}
	for i, node := range cluster {
		node.SetBackend(&BackendFake{nodes: nodes, ProposalTime: time.Millisecond, nodeId: i})
	}
	for i := range cluster {
		cluster[i].RunPrepare(context.Background())
	}

	done := make([]bool, len(cluster))
	creatBlock := func() {
		for {
			st := []string{}
			wg := sync.WaitGroup{}
			for i := range cluster {
				state := cluster[i].GetState()
				if state == pbft.DoneState || state == pbft.SyncState {
					done[i] = true
					fmt.Println("==DONE ", i, state.String())
				} else {
					wg.Add(1)
					go func(i int) {
						fmt.Println("+", i, "RunCycle")
						defer func() {
							fmt.Println("-", i, "RunCycle")
						}()
						defer wg.Done()
						cluster[i].RunCycle(context.Background())
					}(i)
				}
			}
			fmt.Println("wg.wait")
			wg.Wait()
			for i := range cluster {
				state := cluster[i].GetState()
				st = append(st, state.String())
			}

			fmt.Println("states", st)
			stop := true
			for _, b := range done {
				if !b {
					stop = false
					break
				}
			}
			fmt.Println("@@done", done)
			if stop {
				fmt.Println(done)
				break
			}
		}
	}
	creatBlock()
	fmt.Println("=====================")
	for i := range cluster {
		cluster[i].Print()
	}

	creatBlock()
	fmt.Println("=====================")
	for i := range cluster {
		cluster[i].Print()
	}
}

type fakeTracer struct {
}

func (a *fakeTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return context.TODO(), nil
}

type BackendFake struct {
	nodes        []string
	height       uint64
	lastProposer pbft.NodeID
	ProposalTime time.Duration
	nodeId       int
}

func (bf *BackendFake) BuildProposal() (*pbft.Proposal, error) {
	proposal := &pbft.Proposal{
		Data: GenerateProposal(),
		Time: time.Now().Add(1 * bf.ProposalTime),
	}
	proposal.Hash = Hash(proposal.Data)
	return proposal, nil
}

func (bf *BackendFake) Validate(proposal *pbft.Proposal) error {
	//fmt.Println("Validate", proposal.Hash)
	return nil
}

func (bf *BackendFake) Insert(p *pbft.SealedProposal) error {
	//TODO implement me
	//panic("implement me")
	fmt.Println(bf.nodeId, "inserted", p.Number, p.Proposer, p.Proposal.Data)
	return nil
}

func (bf *BackendFake) Height() uint64 {
	return bf.height
}

func (bf *BackendFake) ValidatorSet() pbft.ValidatorSet {
	valsAsNode := []pbft.NodeID{}
	for _, i := range bf.nodes {
		valsAsNode = append(valsAsNode, pbft.NodeID(i))
	}
	vv := valString{
		nodes:        valsAsNode,
		lastProposer: bf.lastProposer,
	}
	return &vv
}

func (bf *BackendFake) Init(info *pbft.RoundInfo) {
}

func (bf *BackendFake) IsStuck(num uint64) (uint64, bool) {
	//TODO implement me
	fmt.Println("IsStuck", bf.height, num)
	panic("IsStuck " + strconv.Itoa(int(num)))
	//return 0, false
}

func (bf *BackendFake) ValidateCommit(from pbft.NodeID, seal []byte) error {
	return nil
}

type clusterSM struct {
	cluster []*pbft.Pbft
}

func (sm *clusterSM) Init(t *rapid.T) {
	ft := &fakeTransport{}
	nodes := []string{}
	createNode := func(name string) *pbft.Pbft {
		node := pbft.New(key(name), ft, pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")), func(config *pbft.Config) {

		}, pbft.WithLogger(log.New(io.Discard, "", 0)))
		nodes = append(nodes, name)
		ft.nodes = append(ft.nodes, node)
		return node
	}
	numOfNodes := rapid.IntRange(1, 30).Draw(t, "num of nodes").(int)
	sm.cluster = make([]*pbft.Pbft, numOfNodes)
	for i := 0; i < numOfNodes; i++ {
		sm.cluster[i] = createNode(strconv.Itoa(i))
	}
	for i, node := range sm.cluster {
		node.SetBackend(&BackendFake{nodes: nodes, ProposalTime: time.Millisecond, nodeId: i})
	}
	for i := range sm.cluster {
		sm.cluster[i].RunPrepare(context.Background())
	}

}
func (sm *clusterSM) Cleanup() {

}
func (sm *clusterSM) Check(t *rapid.T) {

}

func (sm *clusterSM) CreateBlock(t *rapid.T) {
	defer goleak.VerifyNone(t)

	done := make([]bool, len(sm.cluster))
	for {
		wg := sync.WaitGroup{}
		for i := range sm.cluster {
			state := sm.cluster[i].GetState()
			if state == pbft.DoneState || state == pbft.SyncState {
				done[i] = true
			} else {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					//tt := time.Now()
					sm.cluster[i].RunCycle(context.Background())
					//fmt.Println("=======timing ", state, i, time.Since(tt))
				}(i)
			}
		}
		wg.Wait()
		stop := true
		for _, b := range done {
			if !b {
				stop = false
				break
			}
		}

		if stop {
			break
		}
	}

	for i := range sm.cluster {
		state := sm.cluster[i].GetState()
		if state != pbft.DoneState {
			t.Error(state, i)
		}
	}
}

func TestDifferentClusters(t *testing.T) {
	rapid.Check(t, rapid.Run(&clusterSM{}))
}

type roundChangeFSM struct {
	cluster []*pbft.Pbft
}

func (sm *roundChangeFSM) Init(t *rapid.T) {
	numOfNodes := rapid.IntRange(4, 30).Draw(t, "num of nodes").(int)
	acc := rapid.MapOfN(rapid.IntRange(0, numOfNodes-1), rapid.Int(), numOfNodes*2/3+1, numOfNodes).Draw(t, "a").(map[int]int)

	acceptedNodes := []int{}
	for i, _ := range acc {
		acceptedNodes = append(acceptedNodes, i)
	}

	//acceptedNodes := rapid.SliceOfNDistinct(, numOfNodes*2/3+1, numOfNodes).
	//	Draw(t, "generate >2/3+1 connections").([]int)
	fmt.Println(numOfNodes, len(acceptedNodes), acceptedNodes)
	ft := &fakeTransport{
		GossipFunc: func(ft *fakeTransport, msg *pbft.MessageReq) error {
			//ids := rapid.SliceOfN(rapid.IntRange(0, numOfNodes-1), 1, numOfNodes*3/2).Draw(t, "").([]int)

			for _, id := range acceptedNodes {
				ft.nodes[id].PushMessage(msg.Copy())
			}
			return nil
		},
	}
	nodes := []string{}
	createNode := func(name string) *pbft.Pbft {
		node := pbft.New(key(name), ft,
			pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")),
			func(config *pbft.Config) {},
			pbft.WithLogger(log.New(io.Discard, "", 0)),
		)
		nodes = append(nodes, name)
		ft.nodes = append(ft.nodes, node)
		return node
	}

	sm.cluster = make([]*pbft.Pbft, numOfNodes)
	for i := 0; i < numOfNodes; i++ {
		sm.cluster[i] = createNode(strconv.Itoa(i))
	}
	for i, node := range sm.cluster {
		node.SetBackend(&BackendFake{nodes: nodes, ProposalTime: time.Millisecond, nodeId: i})
	}
	for i := range sm.cluster {
		sm.cluster[i].RunPrepare(context.Background())
	}

}
func (sm *roundChangeFSM) Cleanup() {

}
func (sm *roundChangeFSM) Check(t *rapid.T) {

}

func (sm *roundChangeFSM) CreateBlock(t *rapid.T) {
	defer goleak.VerifyNone(t)

	done := make([]bool, len(sm.cluster))
	for {
		wg := errgroup.Group{}
		for i := range sm.cluster {
			state := sm.cluster[i].GetState()
			if state == pbft.DoneState || state == pbft.SyncState {
				done[i] = true
			} else {
				i := i
				wg.Go(func() (err1 error) {

					defer func() {
						if err := recover(); err != nil {
							err1 = fmt.Errorf("%v", err)
						}
					}()
					tt := time.Now()
					sm.cluster[i].RunCycle(context.Background())
					fmt.Println("=======timing ", state, i, time.Since(tt), err1)
					return err1
				})
			}
		}
		if err := wg.Wait(); err != nil {
			fmt.Println("Err, wg.Wait", err)
			t.Fatal(err)
			return
		}

		stop := true
		for _, b := range done {
			if !b {
				stop = false
				break
			}
		}

		if stop {
			break
		}
	}

	for i := range sm.cluster {
		state := sm.cluster[i].GetState()
		if state != pbft.DoneState {
			t.Error(state, i)
		}
	}
}

func TestRoundChangeFSM(t *testing.T) {
	rapid.Check(t, rapid.Run(&roundChangeFSM{}))
}
