package e2e

import (
	"context"
	"fmt"
	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/debug"
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

const waitDuration = 50 * time.Millisecond

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
			//fmt.Println(debug.Line(), msg.Type, msg.From, node.GetValidatorId())
			node.PushMessage(msg.Copy())
		}
	}
	return nil
}

//todo Remove it?
func TestCreateTwoSequentialBlocks_Success(t *testing.T) {
	defer goleak.VerifyNone(t)

	ft := &fakeTransport{}
	nodes := []string{}
	createNode := func(name string) *pbft.Pbft {
		node := pbft.New(key(name), ft, pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")), func(config *pbft.Config) {})
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
				} else {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						cluster[i].RunCycle(context.Background())
					}(i)
				}
			}
			wg.Wait()
			for i := range cluster {
				state := cluster[i].GetState()
				st = append(st, state.String())
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
	}
	creatBlock()
	creatBlock()
}

type BackendFake struct {
	nodes            []string
	height           uint64
	ProposalTime     time.Duration
	nodeId           int
	IsStuckMock      func(num uint64) (uint64, bool)
	ValidatorSetList pbft.ValidatorSet
	ValidatorSetMock func(fake *BackendFake) pbft.ValidatorSet
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
	//fmt.Println(debug.Line(), "Validate", proposal.Hash)
	return nil
}

func (bf *BackendFake) Insert(p *pbft.SealedProposal) error {
	//TODO implement me
	//	fmt.Println(debug.Line(), bf.nodeId, "inserted", p.Number, p.Proposer)
	return nil
}

func (bf *BackendFake) Height() uint64 {
	return bf.height
}

func (bf *BackendFake) ValidatorSet() pbft.ValidatorSet {
	if bf.ValidatorSetMock != nil {
		return bf.ValidatorSetMock(bf)
	}
	valsAsNode := []pbft.NodeID{}
	for _, i := range bf.nodes {
		valsAsNode = append(valsAsNode, pbft.NodeID(i))
	}
	vv := valString{
		nodes: valsAsNode,
	}
	return &vv
}

func (bf *BackendFake) Init(info *pbft.RoundInfo) {
}

func (bf *BackendFake) IsStuck(num uint64) (uint64, bool) {
	if bf.IsStuckMock != nil {
		return bf.IsStuckMock(num)
	}
	panic("IsStuck " + strconv.Itoa(int(num)))
}

func (bf *BackendFake) ValidateCommit(from pbft.NodeID, seal []byte) error {
	return nil
}

func TestSuccess(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		defer goleak.VerifyNone(t)
		numOfNodes := rapid.IntRange(4, 30).Draw(t, "num of nodes").(int)
		ft := &fakeTransport{}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft)
		for i := range cluster {
			cluster[i].RunPrepare(context.Background())
		}

		stuckList := NewBoolSlice(len(cluster))
		doneList := NewBoolSlice(len(cluster))

		callNumber := 0
		for {
			callNumber++
			err := runClusterCycle(cluster, callNumber, stuckList, doneList)
			if err != nil {
				t.Error(err)
			}

			setDoneOnDoneState(cluster, doneList)
			if stuckList.CalculateNum(true) == numOfNodes {
				for i := range timeoutsChan {
					timeoutsChan[i] <- time.Now()
				}
			}

			//check that 3 node switched to done state
			if doneList.CalculateNum(true) == numOfNodes {
				//everything done. Success.
				return
			}

			//something went wrong.
			if getMaxClusterRound(cluster) > 3 {
				t.Error("Infinite rounds")
				return
			}
		}
	})

}

func TestCheckMajorityProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 4).Draw(t, "num of nodes").(int)
		routingMapGenerator := rapid.MapOfN(
			rapid.Uint64Range(0, uint64(numOfNodes)-1),
			rapid.Bool(),
			2*numOfNodes/3+1,
			numOfNodes-1,
		).Filter(func(m map[uint64]bool) bool {
			_, ok := m[0]
			return ok
		})

		routingMap := routingMapGenerator.Draw(t, "generate routing").(map[uint64]bool)

		ft := &fakeTransport{
			GossipFunc: func(ft *fakeTransport, msg *pbft.MessageReq) error {
				from, err := strconv.ParseUint(string(msg.From), 10, 64)
				if err != nil {
					t.Fatal(err)
				}

				if _, ok := routingMap[from]; ok {
					for i := range routingMap {
						if i == from {
							continue
						}
						ft.nodes[i].PushMessage(msg)
					}
				}

				return nil
			},
		}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft)
		for i := range cluster {
			cluster[i].RunPrepare(context.Background())
		}

		stuckList := NewBoolSlice(len(cluster))
		doneList := NewBoolSlice(len(cluster))

		callNumber := 0
		for {
			callNumber++
			err := runClusterCycle(cluster, callNumber, stuckList, doneList)
			if err != nil {
				t.Error(err)
			}

			setDoneOnDoneState(cluster, doneList)
			if stuckList.CalculateNum(true) == numOfNodes {
				for i := range timeoutsChan {
					timeoutsChan[i] <- time.Now()
				}
			}

			//check that 3 node switched to done state
			if doneList.CalculateNum(true) == numOfNodes*2/3+1 {
				//everything done. Success.
				return
			}

			//something went wrong.
			if callNumber == 100 {
				fmt.Println(debug.Line(), "max round", getMaxClusterRound(cluster))
			}

			if getMaxClusterRound(cluster) > 10 {
				t.Error("Infinite rounds")
				fmt.Println(debug.Line(), "getMaxClusterRound exit")
				return
			}
			if callNumber > 100 {
				fmt.Println(debug.Line(), "callNumber exit")
				t.Error("stucked")
				return
			}
		}
	})
}

func TestCheckLivenessIssueCheck(t *testing.T) {
	numOfNodes := 4
	rounds := map[uint64]map[int][]int{
		0: {
			0: {0, 1, 3, 2},
			1: {0, 1, 3},
			2: {0, 1, 2, 3},
		},
	}

	countPrepare := 0
	ft := &fakeTransport{
		GossipFunc: func(ft *fakeTransport, msg *pbft.MessageReq) error {
			routing, changed := rounds[msg.View.Round]
			if changed {
				//todo hack
				from, err := strconv.Atoi(string(msg.From))
				if err != nil {
					t.Fatal(err)
				}
				for _, nodeId := range routing[from] {
					// restrict prepare messages to node 3 in round 0
					if msg.Type == pbft.MessageReq_Prepare && nodeId == 3 {
						countPrepare++
						if countPrepare == 3 {
							fmt.Println("Ignoring prepare 3")
							continue
						}
					}
					// do not send commit for round 0
					if msg.Type == pbft.MessageReq_Commit {
						continue
					}

					//fmt.Println(debug.Line(), "Push", msg.Type, "round", msg.View.Round, "from", msg.From, "to", nodeId)
					ft.nodes[nodeId].PushMessage(msg)

				}
			} else {
				for i := range ft.nodes {
					from, _ := strconv.Atoi(string(msg.From))
					// for rounds >0 do not send messages to/from node 3
					if i == 3 || from == 3 {
						continue
					} else {
						ft.nodes[i].PushMessage(msg)
					}
				}
			}

			return nil
		},
	}
	cluster, timeoutsChan := generateCluster(numOfNodes, ft)
	for i := range cluster {
		cluster[i].RunPrepare(context.Background())
	}

	stuckList := NewBoolSlice(len(cluster))
	doneList := NewBoolSlice(len(cluster))

	callNumber := 0
	for {
		callNumber++
		err := runClusterCycle(cluster, callNumber, stuckList, doneList)
		if err != nil {
			t.Error(err)
		}

		setDoneOnDoneState(cluster, doneList)
		if stuckList.CalculateNum(true) == numOfNodes {
			for i := range timeoutsChan {
				timeoutsChan[i] <- time.Now()
			}
		}

		//check that 3 node switched to done state
		if doneList.CalculateNum(true) >= 3 {
			fmt.Println(debug.Line(), "done")
			return
		}

		if getMaxClusterRound(cluster) > 5 {
			t.Error("Infinite rounds")
			return
		}
	}
}

func TestCheckLivenessIssue2Check(t *testing.T) {
	numOfNodes := 5
	rounds := map[uint64]map[int][]int{
		0: {
			0: {0, 1, 2},
			1: {0},
			2: {0, 3},
			3: {0},
		},
		1: {
			0: {1},
			1: {1, 2, 3},
			2: {1},
			3: {1},
		},
	}

	ft := &fakeTransport{
		GossipFunc: func(ft *fakeTransport, msg *pbft.MessageReq) error {
			routing, changed := rounds[msg.View.Round]
			if changed {
				from, err := strconv.Atoi(string(msg.From))
				if err != nil {
					t.Fatal(err)
				}
				for _, nodeId := range routing[from] {
					fmt.Println(debug.Line(), "Push", msg.Type, "round", msg.View.Round, "from", msg.From, "to", nodeId)
					ft.nodes[nodeId].PushMessage(msg)
				}
			} else {
				for i := range ft.nodes {
					ft.nodes[i].PushMessage(msg)
				}
			}
			return nil
		},
	}
	cluster, timeoutsChan := generateCluster(numOfNodes, ft)
	for i := range cluster {
		cluster[i].RunPrepare(context.Background())
	}

	stuckList := NewBoolSlice(len(cluster))
	doneList := NewBoolSlice(len(cluster))

	callNumber := 0
	for {
		callNumber++
		err := runClusterCycle(cluster, callNumber, stuckList, doneList)
		if err != nil {
			t.Error(err)
		}

		setDoneOnDoneState(cluster, doneList)
		if stuckList.CalculateNum(true) == numOfNodes {
			for i := range timeoutsChan {
				timeoutsChan[i] <- time.Now()
			}
		}

		//check that 3 node switched to done state
		if doneList.CalculateNum(true) >= 3 {
			fmt.Println(debug.Line(), "done")
			return
		}

		if getMaxClusterRound(cluster) > 20 {
			t.Error("Infinite rounds")
			return
		}
	}
}

func getMaxClusterRound(cluster []*pbft.Pbft) uint64 {
	var maxRound uint64
	for i := range cluster {
		localRound := cluster[i].Round()
		if localRound > maxRound {
			maxRound = localRound
		}
	}
	return maxRound
}

func NewBoolSlice(ln int) *BoolSlice {
	return &BoolSlice{
		slice: make([]bool, ln),
	}
}

type BoolSlice struct {
	slice []bool
	mtx   sync.RWMutex
}

func (bs *BoolSlice) Set(i int, b bool) {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()
	bs.slice[i] = b
}
func (bs *BoolSlice) Get(i int) bool {
	bs.mtx.Lock()
	defer bs.mtx.Unlock()
	return bs.slice[i]
}

func (bs *BoolSlice) Iterate(f func(k int, v bool)) {
	bs.mtx.RUnlock()
	defer bs.mtx.RUnlock()
	for k, v := range bs.slice {
		f(k, v)
	}
}

func (bs *BoolSlice) CalculateNum(val bool) int {
	bs.mtx.RLock()
	defer bs.mtx.RUnlock()
	nm := 0
	for _, v := range bs.slice {
		if v == val {
			nm++
		}
	}
	return nm
}

func (bs *BoolSlice) String() string {
	bs.mtx.RLock()
	defer bs.mtx.RUnlock()
	return fmt.Sprintf("%v", bs.slice)
}

func generateNode(id int, transport *fakeTransport) (*pbft.Pbft, chan time.Time) {
	node := pbft.New(key(strconv.Itoa(id)), transport,
		pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")),
		func(config *pbft.Config) {},
		pbft.WithLogger(log.New(io.Discard, "", 0)),
	)

	timeoutChan := make(chan time.Time)
	// round timeout mock
	node.SetRoundTimeoutFunction(func() <-chan time.Time {
		return timeoutChan
	})

	transport.nodes = append(transport.nodes, node)
	return node, timeoutChan
}

func generateCluster(numOfNodes int, transport *fakeTransport) ([]*pbft.Pbft, []chan time.Time) {
	nodes := make([]string, numOfNodes)
	timeoutsChan := make([]chan time.Time, numOfNodes)
	cluster := make([]*pbft.Pbft, numOfNodes)
	for i := 0; i < numOfNodes; i++ {
		cluster[i], timeoutsChan[i] = generateNode(i, transport)
		nodes[i] = strconv.Itoa(i)
	}

	for i, node := range cluster {
		i := i
		node := node

		valsAsNode := []pbft.NodeID{}
		for _, i := range nodes {
			valsAsNode = append(valsAsNode, pbft.NodeID(i))
		}
		vv := valString{
			nodes: valsAsNode,
		}

		node.SetBackend(&BackendFake{
			nodes:  nodes,
			nodeId: i,
			IsStuckMock: func(num uint64) (uint64, bool) {
				//fmt.Println(debug.Line(), "check is Stuck", i, num)
				return 0, false
			},
			ValidatorSetList: &vv,
		})
	}
	return cluster, timeoutsChan
}

func runClusterCycle(cluster []*pbft.Pbft, callNumber int, stuckList, doneList *BoolSlice) error {
	wg := errgroup.Group{}
	for i := range cluster {
		i := i
		state := cluster[i].GetState()
		wg.Go(func() (err1 error) {
			wgTime := time.Now()
			exitCh := make(chan struct{})
			deadlineTimeout := waitDuration
			deadline := time.After(deadlineTimeout)
			if stuckList.Get(i) {
				return nil
			}

			if doneList.Get(i) {
				return nil
			}

			go func() {
				stuckList.Set(i, true)
				defer func() {
					stuckList.Set(i, false)
				}()
				cluster[i].RunCycle(context.Background())
				close(exitCh)
			}()
			select {
			case <-exitCh:
			case <-deadline:
			}

			//useful for debug
			_, _ = state, wgTime
			//if time.Since(wgTime) > waitDuration {
			//	fmt.Println(debug.Line(), "wgitme ", state, i, callNumber, time.Since(wgTime), err1)
			//}

			return err1
		})
	}

	return wg.Wait()
}

func setDoneOnDoneState(cluster []*pbft.Pbft, doneList *BoolSlice) {
	for i, node := range cluster {
		state := node.GetState()
		if state == pbft.DoneState {
			doneList.Set(i, true)
		}
	}
}

func maxPredefinedRound(mp map[uint64]map[uint64][]uint64) uint64 {
	var max uint64
	for i := range mp {
		if i > max {
			max = i
		}
	}
	return max
}

//generateRoundsRoutingMap generator for routing map
//map[uint64]map[uint64][]uint64
func generateRoundsRoutingMap(numOfNodes uint64) *rapid.Generator {
	return rapid.MapOfN(
		//round number
		rapid.Uint64Range(0, 3),
		//validator routing
		generateRoutingMap(numOfNodes, 0),
		//min num of rounds
		1,
		//max num of rounds
		10)
}

func generateRoutingMap(numberOfNode uint64, minNumberOfConnections int) *rapid.Generator {
	maxNodeID := numberOfNode - 1
	return rapid.MapOfN(
		//nodes from
		rapid.Uint64Range(uint64(minNumberOfConnections)-1, maxNodeID),
		//nodes to
		rapid.SliceOfNDistinct(rapid.Uint64Range(uint64(minNumberOfConnections)-1, maxNodeID),
			minNumberOfConnections,
			int(maxNodeID),
			nil,
		),
		//min number of connections
		minNumberOfConnections,
		//numOfConnections
		int(maxNodeID),
	)
}

func TestCheckLivenessPropertyCheckGeneratedRouting(t *testing.T) {
	//todo not ready
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 5).Draw(t, "number of nodes").(int)
		rounds := generateRoundsRoutingMap(uint64(numOfNodes)).Draw(t, "generate routes").(map[uint64]map[uint64][]uint64)
		countPrepare := 0
		ft := &fakeTransport{
			GossipFunc: func(ft *fakeTransport, msg *pbft.MessageReq) error {
				routing, changed := rounds[msg.View.Round]
				if changed {
					//todo hack
					from, err := strconv.ParseUint(string(msg.From), 10, 64)
					if err != nil {
						t.Fatal(err)
					}
					for _, nodeId := range routing[from] {
						// restrict prepare messages to node 3 in round 0
						if msg.Type == pbft.MessageReq_Prepare && nodeId == 3 {
							countPrepare++
							if countPrepare == 3 {
								fmt.Println("Ignoring prepare 3")
								continue
							}
						}
						// do not send commit for round 0
						if msg.Type == pbft.MessageReq_Commit {
							continue
						}

						//fmt.Println(debug.Line(), "Push", msg.Type, "round", msg.View.Round, "from", msg.From, "to", nodeId)
						ft.nodes[nodeId].PushMessage(msg)
					}
				} else {
					for i := range ft.nodes {
						//from, _ := strconv.Atoi(string(msg.From))
						//// for rounds >0 do not send messages to/from node 3
						//if i == 3 || from == 3 {
						//	fmt.Println(debug.Line(), "Node 3 ignored")
						//} else {
						ft.nodes[i].PushMessage(msg)
						//}
					}
				}

				return nil
			},
		}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft)
		for i := range cluster {
			cluster[i].RunPrepare(context.Background())
		}

		stuckList := NewBoolSlice(len(cluster))
		doneList := NewBoolSlice(len(cluster))

		callNumber := 0
		for {
			callNumber++
			//fmt.Println(debug.Line(), "call", callNumber, " -----------------------------------", stuckList)
			err := runClusterCycle(cluster, callNumber, stuckList, doneList)
			if err != nil {
				t.Error(err)
			}

			setDoneOnDoneState(cluster, doneList)
			if stuckList.CalculateNum(true) == numOfNodes {
				for i := range timeoutsChan {
					fmt.Println(debug.Line(), "+send timeout", i, cluster[i].GetState())
					select {
					case timeoutsChan[i] <- time.Now():
					default:
						fmt.Println(debug.Line(), "timeout havent send")
					}
					fmt.Println(debug.Line(), "-sent timeout", i)
				}

				fmt.Println("wait unstuck", stuckList)
				i := 0
				for {
					if stuckList.CalculateNum(true) == 0 {
						break
					}
					if i%10 == 0 {
						fmt.Println("stucked", stuckList)
						for j := range cluster {
							fmt.Println(debug.Line(), cluster[j].GetState())
						}
						time.Sleep(time.Millisecond * 50)
					}
					if i > 50 {
						t.Error("stucked")
						return
					}
					i++

				}
			}

			//check that majority switched to done state
			if doneList.CalculateNum(true) >= 3 {
				fmt.Println(debug.Line(), "done")
				return
			}

			//check that we dont stuck after routing rounds
			if getMaxClusterRound(cluster) > maxPredefinedRound(rounds)+2 {
				t.Error("Infinite rounds")
				return
			}
		}
	})
}
