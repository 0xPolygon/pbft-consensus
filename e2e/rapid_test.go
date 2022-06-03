package e2e

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"github.com/0xPolygon/pbft-consensus"
	"go.opentelemetry.io/otel/trace"
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
			node.PushMessage(msg.Copy())
		}
	}
	return nil
}

type BackendFake struct {
	nodes            []string
	height           uint64
	ProposalTime     time.Duration
	nodeId           int
	IsStuckMock      func(num uint64) (uint64, bool)
	ValidatorSetList pbft.ValidatorSet
	ValidatorSetMock func(fake *BackendFake) pbft.ValidatorSet
	finalProposals   *finalProposal
}

func (bf *BackendFake) BuildProposal() (*pbft.Proposal, error) {
	proposal := &pbft.Proposal{
		Data: GenerateProposal(),
		Time: time.Now(),
	}
	proposal.Hash = Hash(proposal.Data)
	return proposal, nil
}

func (bf *BackendFake) Validate(proposal *pbft.Proposal) error {
	return nil
}

func (bf *BackendFake) Insert(p *pbft.SealedProposal) error {
	return bf.finalProposals.Insert(*p)
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

func TestPropertySeveralHonestNodesCanAchiveAgreement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 30).Draw(t, "num of nodes").(int)
		ft := &fakeTransport{}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft)
		for i := range cluster {
			cluster[i].SetInitialState(context.Background())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := runCluster(ctx,
			cluster,
			sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
			func(doneList *BoolSlice) bool {
				//everything done. All nodes in done state
				if doneList.CalculateNum(true) == numOfNodes {
					return true
				}
				return false
			}, func(maxRound uint64) bool {
				//something went wrong.
				if maxRound > 3 {
					t.Error("Infinite rounds")
					return true
				}
				return false
			}, 100)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestPropertySeveralNodesCanAchiveAgreementWithFailureNodes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 30).Draw(t, "num of nodes").(int)
		routingMapGenerator := rapid.MapOfN(
			rapid.Uint64Range(0, uint64(numOfNodes)-1),
			//not used
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
			cluster[i].SetInitialState(context.Background())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := runCluster(ctx,
			cluster,
			sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
			func(doneList *BoolSlice) bool {
				//check that 3 node switched to done state
				if doneList.CalculateNum(true) >= numOfNodes*2/3+1 {
					//everything done. Success.
					return true
				}
				return false
			}, func(maxRound uint64) bool {
				if maxRound > 10 {
					t.Error("Infinite rounds")
					return true
				}
				return false
			}, 100)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestProperty4NodesCanAchiveAgreementIfWeLockButNotCommitProposer_Fails(t *testing.T) {
	t.Skip("Unskip when fix")
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
		//for round 0 we have a routing from routing map without commit messages and
		//for other rounds we dont send messages to node 3
		GossipFunc: func(ft *fakeTransport, msg *pbft.MessageReq) error {
			routing, changed := rounds[msg.View.Round]
			if changed {
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
		cluster[i].SetInitialState(context.Background())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := runCluster(ctx,
		cluster,
		sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
		func(doneList *BoolSlice) bool {
			if doneList.CalculateNum(true) >= 3 {
				return true
			}
			return false
		}, func(maxRound uint64) bool {
			if maxRound > 5 {
				t.Error("Liveness issue")
				return true
			}
			return false
		}, 50)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFiveNodesCanAchiveAgreementIfWeLockTwoNodesOnDifferentProposals(t *testing.T) {
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
		cluster[i].SetInitialState(context.Background())
	}

	err := runCluster(context.Background(),
		cluster,
		sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
		func(doneList *BoolSlice) bool {
			if doneList.CalculateNum(true) > 3 {
				return true
			}
			return false
		}, func(maxNodeRound uint64) bool {
			if maxNodeRound > 20 {
				t.Fatal("too many rounds")
			}
			return true
		}, 0)
	if err != nil {
		t.Fatal(err)
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
	timeoutChan := make(chan time.Time)
	node := pbft.New(key(strconv.Itoa(id)), transport,
		pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")),
		pbft.WithLogger(log.New(io.Discard, "", 0)),
		pbft.WithRoundTimeout(func(_ uint64) <-chan time.Time {
			return timeoutChan
		}),
	)

	transport.nodes = append(transport.nodes, node)
	return node, timeoutChan
}

func generateCluster(numOfNodes int, transport *fakeTransport) ([]*pbft.Pbft, []chan time.Time) {
	var ip = &finalProposal{
		lock: sync.Mutex{},
		bc:   make(map[uint64]pbft.Proposal),
	}

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
			finalProposals: ip,
			nodes:          nodes,
			nodeId:         i,
			IsStuckMock: func(num uint64) (uint64, bool) {
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
		isLocked := cluster[i].IsLocked()
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
			_, _, _ = state, wgTime, isLocked
			//if time.Since(wgTime) > waitDuration {
			//	fmt.Println("wgitme ", state, i, callNumber, time.Since(wgTime), err1, isLocked)
			//}
			//
			return err1
		})
	}

	return wg.Wait()
}

func TestPropertyDoubleSign(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 9).Draw(t, "num of nodes").(int)

		maliciousNode := pbft.NodeID("0")
		// proposal to 1/2 of the nodes
		maliciousProposal := []byte{110, 89, 24, 11}

		ft := &fakeTransport{
			GossipFunc: func(ft *fakeTransport, msg *pbft.MessageReq) error {
				for to := range ft.nodes {
					modifiedMessage := msg.Copy()
					////faulty node sends one proposal to one half of the nodes and malicious on other half
					if modifiedMessage.From == maliciousNode {
						if numOfNodes/2 < to {
							modifiedMessage.Proposal = maliciousProposal
							h := sha1.New()
							h.Write(modifiedMessage.Proposal)
							modifiedMessage.Hash = h.Sum(nil) // hash is [55 127 129 232 88...]
						}
					}

					ft.nodes[to].PushMessage(modifiedMessage)
				}

				return nil
			},
		}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft)
		for i := range cluster {
			cluster[i].SetInitialState(context.Background())
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
			if doneList.CalculateNum(true) >= numOfNodes*2/3+1 {
				//everything done. Success.
				return
			}

			//something went wrong.
			if getMaxClusterRound(cluster) > 10 {
				t.Error("Infinite rounds")
				return
			}
			if callNumber > 100 {
				t.Error("stucked")
				return
			}
		}
	})
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

//inter
type Errorer interface {
	Error(args ...interface{})
}

func sendTimeoutIfNNodesStucked(t Errorer, timeoutsChan []chan time.Time, numOfNodes int) func(stuckList *BoolSlice) bool {
	return func(stuckList *BoolSlice) bool {
		if stuckList.CalculateNum(true) == numOfNodes {
			c := time.After(time.Second * 5)
			for i := range timeoutsChan {
				select {
				case timeoutsChan[i] <- time.Now():
				case <-c:
					t.Error(i, "node timeout stucked")
					return true
				}
			}
		}
		return false
	}
}

func runCluster(ctx context.Context,
	cluster []*pbft.Pbft,
	handleStuckList func(*BoolSlice) bool,
	handleDoneList func(*BoolSlice) bool,
	handleMaxRoundNumber func(uint64) bool,
	limitCallNumber int,
) error {
	for i := range cluster {
		cluster[i].SetInitialState(context.Background())
	}

	stuckList := NewBoolSlice(len(cluster))
	doneList := NewBoolSlice(len(cluster))

	callNumber := 0
	for {
		callNumber++
		err := runClusterCycle(cluster, callNumber, stuckList, doneList)
		if err != nil {
			return err
		}

		setDoneOnDoneState(cluster, doneList)
		if handleStuckList(stuckList) {
			return nil
		}
		if handleDoneList(doneList) {
			return nil
		}

		if handleMaxRoundNumber(getMaxClusterRound(cluster)) {
			return nil
		}
		if callNumber > limitCallNumber {
			return errors.New("callnumber limit")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:

		}
	}
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

func (i *finalProposal) Insert(proposal pbft.SealedProposal) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	if p, ok := i.bc[proposal.Number]; ok {
		fmt.Printf("So far inserted:%v, trying: %v\n", p, proposal.Proposal)

		if !p.Equal(proposal.Proposal) {
			panic("Wrong proposal inserted.")
		}
	} else {
		i.bc[proposal.Number] = *proposal.Proposal
	}
	return nil
}

// finalProposal struct contains inserted final proposals for the node
type finalProposal struct {
	lock sync.Mutex
	bc   map[uint64]pbft.Proposal
}
