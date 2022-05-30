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
	"sort"
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
		ft := &fakeTransport{}
		nodes := []string{}
		createNode := func(name string) *pbft.Pbft {
			node := pbft.New(key(name), ft, pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")), func(config *pbft.Config) {

			}, pbft.WithLogger(log.New(io.Discard, "", 0)))
			nodes = append(nodes, name)
			ft.nodes = append(ft.nodes, node)
			return node
		}
		numOfNodes := rapid.IntRange(4, 10).Draw(t, "num of nodes").(int)
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

		defer goleak.VerifyNone(t)

		//fmt.Println(debug.Line(), "start cycle")
		done := make([]bool, len(cluster))
		for {
			wg := sync.WaitGroup{}
			for i := range cluster {
				state := cluster[i].GetState()
				if state == pbft.DoneState || state == pbft.SyncState {
					done[i] = true
				} else {
					wg.Add(1)
					go func(i int) {
						defer wg.Done()
						//tt := time.Now()
						cluster[i].RunCycle(context.Background())
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

		for i := range cluster {
			state := cluster[i].GetState()
			if state != pbft.DoneState {
				t.Error(state, i)
			}
		}
	})

}

func TestCheckMajorityProperty(t *testing.T) {
	//params
	roundTimeout := time.Millisecond * 5000
	proposalTime := time.Duration(0)

	maxCycles := 30
	rapid.Check(t, func(t *rapid.T) {
		//init
		//todo changing to 10 make test fail.
		numOfNodes := rapid.IntRange(4, 9).Draw(t, "num of nodes").(int)
		acc := rapid.MapOfN(
			rapid.IntRange(0, numOfNodes-1),
			rapid.IntRange(0, numOfNodes-1),
			//numOfNodes/2,
			numOfNodes*2/3+1,
			numOfNodes).Filter(func(m map[int]int) bool {

			from := map[int]struct{}{}
			to := map[int]struct{}{}
			for k, v := range m {
				from[k] = struct{}{}
				to[v] = struct{}{}
			}

			//todo check that 0 is exsist. tmp solution
			if _, ok := from[0]; !ok {
				return false
			}
			//check 2/3+1 property
			if len(from) >= numOfNodes*2/3 && len(to) >= numOfNodes*2/3+1 {
				return true
			}
			return false
		}).Draw(t, "Generate flow map").(map[int]int)
		acceptedNodes := []int{}
		for i := range acc {
			acceptedNodes = append(acceptedNodes, i)
		}
		acceptedNodesMap := map[pbft.NodeID]struct{}{}
		for i := range acc {
			acceptedNodesMap[key(strconv.Itoa(i)).NodeID()] = struct{}{}
		}
		accLock := sync.RWMutex{}
		sort.Ints(acceptedNodes)
		//fmt.Println("Flow map", numOfNodes, acceptedNodes)
		ft := &fakeTransport{
			GossipFunc: func(ft *fakeTransport, msg *pbft.MessageReq) error {
				if msg.Type == pbft.MessageReq_RoundChange {
					//fmt.Println(debug.Line(), "Round change", msg.From)
				}

				for _, node := range ft.nodes {
					accLock.RLock()
					_, okReceiver := acceptedNodesMap[node.GetValidatorId()]
					_, okSender := acceptedNodesMap[msg.From]
					//fmt.Println(debug.Line(), "send ", msg.Type.String(), " from ", msg.From, "to", node.GetValidatorId(), okSender && okReceiver && node.GetValidatorId() != msg.From)
					//fmt.Println(debug.Line(), "send ", msg.Type.String(), " from ", msg.From, "to", node.GetValidatorId(), okSender && okReceiver)
					//if okSender && okReceiver && node.GetValidatorId() != msg.From {
					if okSender && okReceiver {
						node.PushMessage(msg.Copy())
					}
					accLock.RUnlock()
				}
				return nil
			},
		}
		nodes := []string{}
		createNode := func(name string) *pbft.Pbft {
			node := pbft.New(key(name), ft,
				pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")),
				func(config *pbft.Config) {
					config.RoundTimeout = func(u uint64) time.Duration {
						return roundTimeout
					}
				},
				pbft.WithLogger(log.New(io.Discard, "", 0)),
			)
			// round timeout mock
			node.SetRoundTimeoutFunction(func() <-chan time.Time {
				return make(<-chan time.Time)
			})
			nodes = append(nodes, name)
			ft.nodes = append(ft.nodes, node)
			return node
		}

		cluster := make([]*pbft.Pbft, numOfNodes)
		for i := 0; i < numOfNodes; i++ {
			cluster[i] = createNode(strconv.Itoa(i))
		}
		stuckList := make([]bool, len(cluster))
		for i, node := range cluster {
			i := i
			node := node
			node.SetBackend(&BackendFake{nodes: nodes, ProposalTime: proposalTime, nodeId: i, IsStuckMock: func(num uint64) (uint64, bool) {
				fmt.Println(debug.Line(), "is Stuck", i)
				if _, ok := acceptedNodesMap[node.GetValidatorId()]; !ok {
					return 0, true
				}
				//t.Error(i, num, acceptedNodesMap)
				return 0, false
			}})
		}
		for i := range cluster {
			cluster[i].RunPrepare(context.Background())
		}

		//act
		done := make([]bool, len(cluster))
		numOfCycles := 0
		for {
			numOfCycles++
			wg := errgroup.Group{}
			for i := range cluster {
				state := cluster[i].GetState()
				if state == pbft.DoneState {
					done[i] = true
				} else {
					i := i
					wg.Go(func() (err1 error) {
						wgTime := time.Now()
						exitCh := make(chan struct{})
						deadlineTimeout := time.Millisecond * 50
						deadline := time.After(deadlineTimeout)
						if stuckList[i] {
							return nil
						}
						go func() {
							cluster[i].RunCycle(context.Background())
							close(exitCh)
						}()
						select {
						case <-exitCh:
						case <-deadline:
							stuckList[i] = true
						}

						if time.Since(wgTime).Milliseconds() > 400 {
							fmt.Println(debug.Line(), "wgitme ", state, i, time.Since(wgTime), err1)
							if state == pbft.ValidateState {
								cluster[i].Print()
							}
						}

						return err1
					})
				}
			}
			//fmt.Println(debug.Line(), "wg.Wait()")
			if err := wg.Wait(); err != nil {
				fmt.Println("Err, wg.Wait", err)
				t.Error("wg wain", err)
				//return
			}

			stop := true
			for i, b := range done {
				if _, ok := acceptedNodesMap[key(strconv.Itoa(i)).NodeID()]; !ok {
					continue
				}
				if !b {
					stop = false
					break
				}
			}

			numOfRoundChange := 0
			listOfRoundChangeState := []int{}
			for i, node := range cluster {
				if node.GetState() == pbft.RoundChangeState {
					numOfRoundChange++
					listOfRoundChangeState = append(listOfRoundChangeState, i)
				}
			}
			if checkNumTrue(stuckList, 3) {
				fmt.Println(debug.Line(), "stucked", stuckList)
				t.Error("stucked", stuckList)
				stop = true

			}
			if numOfRoundChange >= numOfNodes*2/3 {
				fmt.Println(debug.Line(), "numOfNodes exit", numOfRoundChange, numOfNodes, numOfNodes/3*2)
				t.Error(listOfRoundChangeState)
				stop = true
			}

			if numOfCycles > maxCycles {
				fmt.Println(debug.Line(), "Max cycles run", numOfCycles)
				t.Error("Max cycles run")
				stop = true
			}
			if stop {
				//fmt.Println(debug.Line(), "stop", done, checkNumTrue(stuckList, 3), numOfRoundChange >= numOfNodes*2/3)
				break
			}
		}

		//fmt.Println(debug.Line(), "NumOfCycles", numOfCycles)
		for i := range cluster {
			state := cluster[i].GetState()
			if _, ok := acceptedNodesMap[cluster[i].GetValidatorId()]; !ok {
				continue
			}
			if state != pbft.DoneState {
				fmt.Println(debug.Line(), "errorStop", i, state)
				t.Error(state, i)
			}
		}
	})
}

func checkNumTrue(b []bool, num int) bool {
	nm := 0
	for _, v := range b {
		if v {
			nm++
		}
	}
	return nm >= num
}

func TestCheckLivenessBugProperty(t *testing.T) {
	//t.Skip()
	//params
	roundTimeout := time.Millisecond * 5000
	proposalTime := time.Duration(0)

	maxCycles := 50
	rapid.Check(t, func(t *rapid.T) {
		fmt.Println(debug.Line(), "------------------------")
		//init
		//changing to 10 make test fail.
		numOfNodes := rapid.IntRange(4, 4).Draw(t, "num of nodes").(int)
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
				//todo add routing
				routing := rounds[msg.View.Round]
				//todo hack
				from, err := strconv.Atoi(string(msg.From))
				if err != nil {
					t.Fatal(err)
				}
				for _, nodeId := range routing[from] {
					//fmt.Println(debug.Line(), "Push", msg.Type, "round", msg.View.Round, "from", msg.From, "to", nodeId)
					ft.nodes[nodeId].PushMessage(msg)
				}
				return nil
			},
		}

		timeoutsChan := make([]chan time.Time, numOfNodes)
		nodes := []string{}
		createNode := func(name string) *pbft.Pbft {
			node := pbft.New(key(name), ft,
				pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")),
				func(config *pbft.Config) {
					config.RoundTimeout = func(u uint64) time.Duration {
						return roundTimeout
					}
				},
				pbft.WithLogger(log.New(io.Discard, "", 0)),
			)
			nodeID, _ := strconv.Atoi(name)
			timeoutsChan[nodeID] = make(chan time.Time)
			// round timeout mock
			node.SetRoundTimeoutFunction(func() <-chan time.Time {
				return timeoutsChan[nodeID]
			})
			nodes = append(nodes, name)
			ft.nodes = append(ft.nodes, node)
			return node
		}

		cluster := make([]*pbft.Pbft, numOfNodes)
		for i := 0; i < numOfNodes; i++ {
			cluster[i] = createNode(strconv.Itoa(i))
		}
		stuckList := make([]bool, len(cluster))
		for i, node := range cluster {
			i := i
			node := node
			node.SetBackend(&BackendFake{nodes: nodes, ProposalTime: proposalTime, nodeId: i, IsStuckMock: func(num uint64) (uint64, bool) {
				fmt.Println(debug.Line(), "check is Stuck", i)
				return 0, false
			}})
		}
		for i := range cluster {
			cluster[i].RunPrepare(context.Background())
		}

		done := make([]bool, len(cluster))
		numOfCycles := 0
		for {
			numOfCycles++
			fmt.Println(debug.Line(), "Cycle", numOfCycles)
			wg := errgroup.Group{}
			for i := range cluster {
				state := cluster[i].GetState()
				if state == pbft.DoneState {
					done[i] = true
				} else {
					i := i
					wg.Go(func() (err1 error) {
						wgTime := time.Now()
						exitCh := make(chan struct{})
						deadlineTimeout := time.Millisecond * 50
						deadline := time.After(deadlineTimeout)
						if stuckList[i] {
							return nil
						}
						go func() {
							cluster[i].RunCycle(context.Background())
							close(exitCh)
						}()
						select {
						case <-exitCh:
						case <-deadline:
							stuckList[i] = true
						}

						if time.Since(wgTime).Milliseconds() > 400 {
							fmt.Println(debug.Line(), "wgitme ", state, i, time.Since(wgTime), err1)
							if state == pbft.ValidateState {
								cluster[i].Print()
							}
						}

						return err1
					})
				}
			}

			if err := wg.Wait(); err != nil {
				fmt.Println("Err, wg.Wait", err)
				t.Error("wg wain", err)
			}

			//Exit cycle condition
			stop := true
			for _, b := range done {
				//todo add not done node as exceptions
				if !b {
					stop = false
					break
				}
			}

			numOfRoundChange := 0
			listOfRoundChangeState := []int{}
			for i, node := range cluster {
				if node.GetState() == pbft.RoundChangeState {
					numOfRoundChange++
					listOfRoundChangeState = append(listOfRoundChangeState, i)
				}
			}
			if checkNumTrue(stuckList, numOfNodes) {
				for i := range cluster {
					fmt.Println(debug.Line(), "checkNumTrue", i, cluster[i].GetState())
				}
				fmt.Println(debug.Line(), "stucked push round timeout", stuckList)
				for i := range timeoutsChan {
					i := i
					end := make(chan struct{})
					go func() {
						select {
						case <-time.After(time.Millisecond * 100):
							fmt.Println(debug.Line(), "i", i, "stucked", cluster[i].GetState())
						case <-end:

						}

					}()
					timeoutsChan[i] <- time.Now()
					close(end)

					//}()

				}
				stuckList = make([]bool, numOfNodes)
				//fmt.Println(debug.Line(), "stucked", stuckList)
				//t.Error("stucked", stuckList)
				//stop = true

			}
			//if numOfRoundChange >= numOfNodes*2/3 {
			//	fmt.Println(debug.Line(), "STOP numOfNodes exit", numOfRoundChange, numOfNodes, numOfNodes/3*2)
			//	t.Error(listOfRoundChangeState)
			//	stop = true
			//}

			if numOfCycles > maxCycles {
				fmt.Println(debug.Line(), "STOP Max cycles run", numOfCycles)
				t.Error("Max cycles run")
				stop = true
			}
			if stop {
				break
			}
		}

		for i := range cluster {
			state := cluster[i].GetState()
			//todo exclude stucked nodes

			if state != pbft.DoneState {
				fmt.Println(debug.Line(), "errorStop", i, state)
				t.Error(state, i)
			}
		}
	})
}

func TestCheckLivenessBugPropertyDebug(t *testing.T) {
	//params
	roundTimeout := time.Millisecond * 5000
	proposalTime := time.Duration(0)

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
				//todo hack
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

	timeoutsChan := make([]chan time.Time, numOfNodes)
	nodes := []string{}
	createNode := func(name string) *pbft.Pbft {
		node := pbft.New(key(name), ft,
			pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")),
			func(config *pbft.Config) {
				config.RoundTimeout = func(u uint64) time.Duration {
					return roundTimeout
				}
			},
			pbft.WithLogger(log.New(io.Discard, "", 0)),
		)
		nodeID, _ := strconv.Atoi(name)
		timeoutsChan[nodeID] = make(chan time.Time)
		// round timeout mock
		node.SetRoundTimeoutFunction(func() <-chan time.Time {
			return timeoutsChan[nodeID]
		})
		nodes = append(nodes, name)
		ft.nodes = append(ft.nodes, node)
		return node
	}

	cluster := make([]*pbft.Pbft, numOfNodes)
	for i := 0; i < numOfNodes; i++ {
		cluster[i] = createNode(strconv.Itoa(i))
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
			nodes:        nodes,
			ProposalTime: proposalTime,
			nodeId:       i,
			IsStuckMock: func(num uint64) (uint64, bool) {
				fmt.Println(debug.Line(), "check is Stuck", i)
				return 0, false
			},
			ValidatorSetList: &vv,
		})
	}
	for i := range cluster {
		cluster[i].RunPrepare(context.Background())
	}

	stuckList := make([]bool, len(cluster))
	stuckListMtx := sync.RWMutex{}
	doneList := make([]bool, len(cluster))
	doneListMtx := sync.RWMutex{}

	for callNumber := 1; callNumber < 30; callNumber++ {
		fmt.Println(debug.Line(), "call", callNumber, " -----------------------------------", stuckList)
		wg := errgroup.Group{}
		for i := range cluster {
			i := i
			state := cluster[i].GetState()
			wg.Go(func() (err1 error) {
				fmt.Println(debug.Line(), callNumber, "started", i, state)
				defer func() {
					fmt.Println(debug.Line(), callNumber, "finished", i, state)
				}()

				wgTime := time.Now()
				exitCh := make(chan struct{})
				deadlineTimeout := time.Millisecond * 50
				deadline := time.After(deadlineTimeout)
				stuckListMtx.Lock()
				if stuckList[i] {
					stuckListMtx.Unlock()
					return nil
				}
				stuckListMtx.Unlock()

				doneListMtx.Lock()
				if doneList[i] {
					doneListMtx.Unlock()
					return nil
				}
				doneListMtx.Unlock()

				go func() {
					stuckListMtx.Lock()
					stuckList[i] = true
					stuckListMtx.Unlock()
					defer func() {
						stuckListMtx.Lock()
						stuckList[i] = false
						stuckListMtx.Unlock()

					}()
					cluster[i].RunCycle(context.Background())
					close(exitCh)
				}()
				select {
				case <-exitCh:
				case <-deadline:
				}

				if time.Since(wgTime).Milliseconds() > 400 {
					fmt.Println(debug.Line(), "wgitme ", state, i, time.Since(wgTime), err1)
					cluster[i].Print()
				}

				return err1
			})
		}

		fmt.Println(debug.Line(), "Wait", callNumber, stuckList)
		if err := wg.Wait(); err != nil {
			fmt.Println("Err, wg.Wait", err)
			t.Error("wg wain", err)
		}

		for i, node := range cluster {
			state := node.GetState()
			fmt.Println(debug.Line(), state, "validator", i)
			if state == pbft.DoneState {
				doneListMtx.Lock()
				doneList[i] = true
				doneListMtx.Unlock()
			}
		}

		stuckListMtx.Lock()
		b := checkNumTrue(stuckList, len(stuckList))
		stuckListMtx.Unlock()
		if b {
			fmt.Println(debug.Line(), "send timeout")
			for i := range timeoutsChan {
				timeoutsChan[i] <- time.Now()
			}
			//for i := range stuckList {
			//	stuckList[i] = false
			//}
		}
	}
}

func TestCheckLivenessBug2Debug(t *testing.T) {
	//params
	roundTimeout := time.Millisecond * 5000
	proposalTime := time.Duration(0)

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
			//fmt.Println("Round ", msg.View.Round)
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

					fmt.Println(debug.Line(), "Push", msg.Type, "round", msg.View.Round, "from", msg.From, "to", nodeId)
					ft.nodes[nodeId].PushMessage(msg)
				}
			} else {
				for i := range ft.nodes {
					from, _ := strconv.Atoi(string(msg.From))
					// for rounds >0 do not send messages to/from node 3
					if i == 3 || from == 3 {
						fmt.Println(debug.Line(), "Node 3 ignored")
					} else {
						ft.nodes[i].PushMessage(msg)
					}
				}

			}

			return nil
		},
	}

	timeoutsChan := make([]chan time.Time, numOfNodes)
	nodes := []string{}
	createNode := func(name string) *pbft.Pbft {
		node := pbft.New(key(name), ft,
			pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")),
			func(config *pbft.Config) {
				config.RoundTimeout = func(u uint64) time.Duration {
					return roundTimeout
				}
			},
			pbft.WithLogger(log.New(io.Discard, "", 0)),
		)
		nodeID, _ := strconv.Atoi(name)
		timeoutsChan[nodeID] = make(chan time.Time)
		// round timeout mock
		node.SetRoundTimeoutFunction(func() <-chan time.Time {
			return timeoutsChan[nodeID]
		})
		nodes = append(nodes, name)
		ft.nodes = append(ft.nodes, node)
		return node
	}

	cluster := make([]*pbft.Pbft, numOfNodes)
	for i := 0; i < numOfNodes; i++ {
		cluster[i] = createNode(strconv.Itoa(i))
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
			nodes:        nodes,
			ProposalTime: proposalTime,
			nodeId:       i,
			IsStuckMock: func(num uint64) (uint64, bool) {
				fmt.Println(debug.Line(), "check is Stuck", i)
				return 0, false
			},
			ValidatorSetList: &vv,
		})
	}
	for i := range cluster {
		cluster[i].RunPrepare(context.Background())
	}

	stuckList := NewBoolSlice(numOfNodes)
	doneList := NewBoolSlice(numOfNodes)

	for callNumber := 1; callNumber < 30; callNumber++ {
		fmt.Println(debug.Line(), "call", callNumber, " -----------------------------------", stuckList)
		wg := errgroup.Group{}
		for i := range cluster {
			i := i
			state := cluster[i].GetState()
			wg.Go(func() (err1 error) {
				fmt.Println(debug.Line(), callNumber, "started", i, state)
				defer func() {
					fmt.Println(debug.Line(), callNumber, "finished", i, state)
				}()

				wgTime := time.Now()
				exitCh := make(chan struct{})
				deadlineTimeout := time.Millisecond * 50
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

				if time.Since(wgTime).Milliseconds() > 400 {
					fmt.Println(debug.Line(), "wgitme ", state, i, time.Since(wgTime), err1)
					cluster[i].Print()
				}

				return err1
			})
		}

		fmt.Println(debug.Line(), "Wait", callNumber, stuckList)
		if err := wg.Wait(); err != nil {
			fmt.Println("Err, wg.Wait", err)
			t.Error("wg wain", err)
		}

		for i, node := range cluster {
			state := node.GetState()
			fmt.Println(debug.Line(), state, "validator", i)
			fmt.Println(debug.Line(), "isLocked", node.IsLocked())
			fmt.Println(debug.Line(), "Sequence", node.Height())
			if state == pbft.DoneState {
				doneList.Set(i, true)
			}
		}

		if stuckList.CalculateNum(true) == numOfNodes {
			fmt.Println(debug.Line(), "send timeout")
			for i := range timeoutsChan {
				timeoutsChan[i] <- time.Now()
				stuckList.Set(i, false)
			}
		}
	}
}

func TestCheckLivenessPropertyCheck(t *testing.T) {
	//rapid.Check(t, func(t *rapid.T) {
	numOfNodes := 4 //rapid.IntRange(4, 4).Draw(t, "number of nodes").(int)
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

					fmt.Println(debug.Line(), "Push", msg.Type, "round", msg.View.Round, "from", msg.From, "to", nodeId)
					ft.nodes[nodeId].PushMessage(msg)

				}
			} else {
				for i := range ft.nodes {
					from, _ := strconv.Atoi(string(msg.From))
					// for rounds >0 do not send messages to/from node 3
					if i == 3 || from == 3 {
						fmt.Println(debug.Line(), "Node 3 ignored")
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
		fmt.Println(debug.Line(), "call", callNumber, " -----------------------------------", stuckList)
		err := runClusterCycle(cluster, callNumber, stuckList, doneList)
		if err != nil {
			t.Error(err)
		}

		setDoneOnDoneState(cluster, doneList)
		if stuckList.CalculateNum(true) == numOfNodes {
			for i := range timeoutsChan {
				fmt.Println(debug.Line(), "send timeout", i)
				timeoutsChan[i] <- time.Now()
				stuckList.Set(i, false)
			}
		}

		//
		if doneList.CalculateNum(true) >= 3 {
			fmt.Println(debug.Line(), "done")
			return
		}
		//todo more generalized check
		for i := range cluster {
			if cluster[i].Round() > 5 {
				t.Error("Infinite rounds")
				return
			}

		}
	}

	//})
}

func TestCheckLivenessPropertyCheck2(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 9).Draw(t, "number of nodes").(int)
		//rounds := map[uint64]map[uint64][]uint64{
		//	0: {
		//		0: {0, 1, 3, 2},
		//		1: {0, 1, 3},
		//		2: {0, 1, 2, 3},
		//	},
		//}

		rounds := generateRoutesMap(t, uint64(numOfNodes)).Draw(t, "generate routes").(map[uint64]map[uint64][]uint64)
		fmt.Println(debug.Line(), "----------------------------------------")
		fmt.Println(debug.Line(), numOfNodes, rounds)
		fmt.Println(debug.Line(), "----------------------------------------")
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

			//
			if doneList.CalculateNum(true) >= 3 {
				fmt.Println(debug.Line(), "done")
				return
			}
			//todo more generalized check
			for i := range cluster {
				if cluster[i].Round() > maxPredefinedRound(rounds)+2 {
					t.Error("Infinite rounds")
					return
				}

			}
		}

	})
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
			//fmt.Println(debug.Line(), callNumber, "started", i, state)
			//defer func() {
			//	fmt.Println(debug.Line(), callNumber, "finished", i, state)
			//}()

			wgTime := time.Now()
			exitCh := make(chan struct{})
			deadlineTimeout := time.Millisecond * 50
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

			if time.Since(wgTime).Milliseconds() > 400 {
				fmt.Println(debug.Line(), "wgitme ", state, i, time.Since(wgTime), err1)
				cluster[i].Print()
			}

			return err1
		})
	}

	//fmt.Println(debug.Line(), "Wait", callNumber, stuckList)
	return wg.Wait()
}

func setDoneOnDoneState(cluster []*pbft.Pbft, doneList *BoolSlice) {
	for i, node := range cluster {
		state := node.GetState()
		//fmt.Println(debug.Line(), state, "validator", i)
		//fmt.Println(debug.Line(), "isLocked", node.IsLocked())
		//fmt.Println(debug.Line(), "Sequence", node.Height())
		//fmt.Println(debug.Line(), "Round", node.Round())
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

func generateRoutesMap(t *rapid.T, numOfNodes uint64) *rapid.Generator {
	maxNodeID := numOfNodes - 1
	return rapid.MapOfN(
		//round number
		rapid.Uint64Range(0, 10),
		//validator routing
		rapid.MapOfN(
			//nodes from
			rapid.Uint64Range(0, maxNodeID),
			//nodes to
			rapid.SliceOfNDistinct(rapid.Uint64Range(0, maxNodeID),
				0,
				int(maxNodeID),
				nil,
			),
			//min number of connections
			0,
			//numOfConnections
			int(maxNodeID),
		),
		//min num of rounds
		1,
		//max num of rounds
		10)
}

//func TestGenerateRouting(t *testing.T) {
//	rapid.Check(t, func(t *rapid.T) {
//		numOfNodes := rapid.IntRange(4, 10).Draw(t, "number of nodes").(int)
//		//fmt.Println(rapid.SliceOfNDistinct(rapid.IntRange(4, numOfNodes), 0, 5, nil).Draw(t, ""))
//
//		//route := generateRoutesMap(t, numOfNodes).Draw(t, "routes")
//		//fmt.Println(route)
//	})
//}
