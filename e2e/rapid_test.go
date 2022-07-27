package e2e

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"pgregory.net/rapid"

	"github.com/0xPolygon/pbft-consensus"
	"github.com/0xPolygon/pbft-consensus/e2e/helper"
)

const waitDuration = 50 * time.Millisecond

func TestProperty_SeveralHonestNodesCanAchiveAgreement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 30).Draw(t, "num of nodes").(int)
		ft := &pbft.TransportStub{}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft, nil)
		for i := range cluster {
			cluster[i].SetInitialState(context.Background())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := runCluster(ctx,
			cluster,
			sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
			func(doneList *helper.BoolSlice) bool {
				//everything done. All nodes in done state
				return doneList.CalculateNum(true) == numOfNodes
			}, func(maxRound uint64) bool {
				// something went wrong.
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

func TestProperty_SeveralNodesCanAchiveAgreementWithFailureNodes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 30).Draw(t, "num of nodes").(int)
		routingMapGenerator := rapid.MapOfN(
			rapid.Uint64Range(0, uint64(numOfNodes)-1),
			// not used
			rapid.Bool(),
			2*numOfNodes/3+1,
			numOfNodes-1,
		).Filter(func(m map[uint64]bool) bool {
			_, ok := m[0]
			return ok
		})

		routingMap := routingMapGenerator.Draw(t, "generate routing").(map[uint64]bool)
		ft := &pbft.TransportStub{
			GossipFunc: func(ft *pbft.TransportStub, msg *pbft.MessageReq) error {
				from, err := strconv.ParseUint(string(msg.From), 10, 64)
				if err != nil {
					t.Fatal(err)
				}

				if _, ok := routingMap[from]; ok {
					for i := range routingMap {
						if i == from {
							continue
						}
						ft.Nodes[i].PushMessage(msg)
					}
				}

				return nil
			},
		}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft, nil)
		for i := range cluster {
			cluster[i].SetInitialState(context.Background())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := runCluster(ctx,
			cluster,
			sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
			func(doneList *helper.BoolSlice) bool {
				//check that 3 node switched to done state
				return doneList.CalculateNum(true) >= numOfNodes*2/3+1
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

func TestProperty_4NodesCanAchiveAgreementIfWeLockButNotCommitProposer_Fails(t *testing.T) {
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
	ft := &pbft.TransportStub{
		// for round 0 we have a routing from routing map without commit messages and
		// for other rounds we dont send messages to node 3
		GossipFunc: func(ft *pbft.TransportStub, msg *pbft.MessageReq) error {
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

					ft.Nodes[nodeId].PushMessage(msg)
				}
			} else {
				for i := range ft.Nodes {
					from, _ := strconv.Atoi(string(msg.From))
					// for rounds >0 do not send messages to/from node 3
					if i == 3 || from == 3 {
						continue
					} else {
						ft.Nodes[i].PushMessage(msg)
					}
				}
			}

			return nil
		},
	}
	cluster, timeoutsChan := generateCluster(numOfNodes, ft, nil)
	for i := range cluster {
		cluster[i].SetInitialState(context.Background())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := runCluster(ctx,
		cluster,
		sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
		func(doneList *helper.BoolSlice) bool {
			return doneList.CalculateNum(true) >= 3
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

func TestProperty_FiveNodesCanAchiveAgreementIfWeLockTwoNodesOnDifferentProposals(t *testing.T) {
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

	ft := &pbft.TransportStub{
		GossipFunc: func(ft *pbft.TransportStub, msg *pbft.MessageReq) error {
			routing, changed := rounds[msg.View.Round]
			if changed {
				from, err := strconv.Atoi(string(msg.From))
				if err != nil {
					t.Fatal(err)
				}
				for _, nodeId := range routing[from] {
					ft.Nodes[nodeId].PushMessage(msg)
				}
			} else {
				for i := range ft.Nodes {
					ft.Nodes[i].PushMessage(msg)
				}
			}
			return nil
		},
	}
	cluster, timeoutsChan := generateCluster(numOfNodes, ft, nil)
	for i := range cluster {
		cluster[i].SetInitialState(context.Background())
	}

	err := runCluster(context.Background(),
		cluster,
		sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
		func(doneList *helper.BoolSlice) bool {
			return doneList.CalculateNum(true) > 3
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

func TestProperty_NodeDoubleSign(t *testing.T) {
	t.Skip("Unskip when fix")
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 7).Draw(t, "num of nodes").(int)
		// sign different message to up to 1/2 of the nodes
		maliciousMessagesToNodes := rapid.IntRange(0, numOfNodes/2).Draw(t, "malicious message to nodes").(int)
		weightedNodes := make(map[pbft.NodeID]uint64, numOfNodes)
		for i := 0; i < numOfNodes; i++ {
			weightedNodes[pbft.NodeID(fmt.Sprintf("NODE_%s", strconv.Itoa(i)))] = 1
		}
		metadata := pbft.NewVotingMetadata(weightedNodes)
		faultyNodes := rapid.IntRange(1, int(metadata.MaxFaultyVotingPower())).Draw(t, "malicious nodes").(int)
		maliciousNodes := generateMaliciousProposers(faultyNodes)
		votingPower := make(map[pbft.NodeID]uint64, numOfNodes)

		for i := 0; i < numOfNodes; i++ {
			votingPower[pbft.NodeID(strconv.Itoa(i))] = 1
		}

		ft := &pbft.TransportStub{
			GossipFunc: func(ft *pbft.TransportStub, msg *pbft.MessageReq) error {
				for to := range ft.Nodes {
					modifiedMessage := msg.Copy()
					// faulty node modifies proposal and sends to subset of nodes (maliciousMessagesToNodes)
					from, _ := strconv.Atoi(string(modifiedMessage.From))
					if from < len(maliciousNodes) {
						if maliciousMessagesToNodes > to {
							modifiedMessage.Proposal = maliciousNodes[from].maliciousProposal
							modifiedMessage.Hash = maliciousNodes[from].maliciousProposalHash
						}
					}
					ft.Nodes[to].PushMessage(modifiedMessage)
				}

				return nil
			},
		}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft, votingPower)
		for i := range cluster {
			cluster[i].SetInitialState(context.Background())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := runCluster(ctx,
			cluster,
			sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
			func(doneList *helper.BoolSlice) bool {
				return doneList.CalculateNum(true) >= 2*numOfNodes/3+1
			}, func(maxRound uint64) bool {
				if maxRound > 10 {
					t.Error("Infinite rounds")
					return true
				}
				return false
			}, 100)
		if err != nil {
			// fail if node inserts different proposal
			t.Fatalf("%v\n", err)
		}
	})
}

func TestProperty_SeveralHonestNodesWithVotingPowerCanAchiveAgreement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(4, 10).Draw(t, "num of nodes").(int)
		votingPowerSlice := rapid.SliceOfN(rapid.Uint64Range(1, math.MaxUint64/uint64(numOfNodes)), numOfNodes, numOfNodes).Draw(t, "voting power").([]uint64)
		votingPower := make(map[pbft.NodeID]uint64, numOfNodes)
		for i := 0; i < numOfNodes; i++ {
			votingPower[pbft.NodeID(strconv.Itoa(i))] = votingPowerSlice[i]
		}
		ft := &pbft.TransportStub{}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft, votingPower)
		for i := range cluster {
			cluster[i].SetInitialState(context.Background())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := runCluster(ctx,
			cluster,
			sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
			func(doneList *helper.BoolSlice) bool {
				// everything done. All nodes in done state
				return doneList.CalculateNum(true) == numOfNodes
			}, func(maxRound uint64) bool {
				// something went wrong.
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

func TestProperty_NodesWithMajorityOfVotingPowerCanAchiveAgreement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		numOfNodes := rapid.IntRange(5, 12).Draw(t, "num of nodes").(int)
		stake := rapid.SliceOfN(rapid.Uint64Range(5, 10), numOfNodes, numOfNodes).Draw(t, "Generate stake").([]uint64)
		votingPower := make(map[pbft.NodeID]uint64, numOfNodes)
		for i := range stake {
			votingPower[pbft.NodeID(strconv.Itoa(i))] = stake[i]
		}
		metadata := pbft.NewVotingMetadata(votingPower)
		connectionsList := rapid.SliceOfDistinct(rapid.IntRange(0, numOfNodes-1), func(v int) int {
			return v
		}).Filter(func(votes []int) bool {
			var votesVP uint64
			for i := range votes {
				votesVP += stake[votes[i]]
			}
			return votesVP >= metadata.QuorumSize()
		}).Draw(t, "Select arbitrary nodes that have majority of voting power").([]int)

		connections := map[pbft.NodeID]struct{}{}
		var topologyVotingPower uint64
		for _, nodeIDInt := range connectionsList {
			connections[pbft.NodeID(strconv.Itoa(nodeIDInt))] = struct{}{}
			topologyVotingPower += stake[nodeIDInt]
		}

		ft := &pbft.TransportStub{
			GossipFunc: func(ft *pbft.TransportStub, msg *pbft.MessageReq) error {
				for _, node := range ft.Nodes {
					// skip faulty nodes
					if _, ok := connections[msg.From]; !ok {
						continue
					}
					if msg.From != node.GetValidatorId() {
						node.PushMessage(msg.Copy())
					}
				}
				return nil
			},
		}
		cluster, timeoutsChan := generateCluster(numOfNodes, ft, votingPower)
		for i := range cluster {
			cluster[i].SetInitialState(context.Background())
		}

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		err := runCluster(ctx,
			cluster,
			sendTimeoutIfNNodesStucked(t, timeoutsChan, numOfNodes),
			func(doneList *helper.BoolSlice) bool {
				accumulatedVotingPower := uint64(0)
				// enough nodes (by their respective voting power) are in done state
				doneList.Iterate(func(index int, isDone bool) {
					if isDone {
						accumulatedVotingPower += votingPower[cluster[index].GetValidatorId()]
					}
				})
				return accumulatedVotingPower >= topologyVotingPower
			}, func(maxRound uint64) bool {
				// something went wrong.
				if maxRound > 3 {
					for i := range cluster {
						fmt.Println(i, cluster[i].GetState())
					}
					t.Error("Infinite rounds")
					return true
				}
				return false
			}, 100)
		if err != nil {
			for i := range cluster {
				fmt.Println(i, cluster[i].GetState())
			}
			t.Fatal(err)
		}
	})
}

type maliciousProposer struct {
	nodeID                pbft.NodeID
	maliciousProposal     []byte
	maliciousProposalHash []byte
}

func generateMaliciousProposers(num int) []maliciousProposer {
	maliciousProposers := make([]maliciousProposer, num)
	for i := 0; i < num; i++ {
		maliciousProposal := helper.GenerateProposal()
		h := sha1.New()
		h.Write(maliciousProposal)
		maliciousProposalHash := h.Sum(nil)
		malicious := maliciousProposer{pbft.NodeID(strconv.Itoa(i)), maliciousProposal, maliciousProposalHash}
		maliciousProposers[i] = malicious
	}

	return maliciousProposers
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

func generateNode(id int, transport *pbft.TransportStub) (*pbft.Pbft, chan time.Time) {
	timeoutChan := make(chan time.Time)
	node := pbft.New(pbft.ValidatorKeyMock(strconv.Itoa(id)), transport,
		pbft.WithTracer(trace.NewNoopTracerProvider().Tracer("")),
		pbft.WithLogger(log.New(io.Discard, "", 0)),
		pbft.WithRoundTimeout(func(_ uint64) <-chan time.Time {
			return timeoutChan
		}),
	)

	transport.Nodes = append(transport.Nodes, node)
	return node, timeoutChan
}

func generateCluster(numOfNodes int, transport *pbft.TransportStub, votingPower map[pbft.NodeID]uint64) ([]*pbft.Pbft, []chan time.Time) {
	nodes := make([]string, numOfNodes)
	timeoutsChan := make([]chan time.Time, numOfNodes)
	ip := &finalProposal{
		lock: sync.Mutex{},
		bc:   make(map[uint64]pbft.Proposal),
	}
	cluster := make([]*pbft.Pbft, numOfNodes)
	for i := 0; i < numOfNodes; i++ {
		cluster[i], timeoutsChan[i] = generateNode(i, transport)
		nodes[i] = strconv.Itoa(i)
	}

	for _, nd := range cluster {
		_ = nd.SetBackend(&BackendFake{
			nodes:          nodes,
			votingPowerMap: votingPower,
			insertFunc: func(proposal *pbft.SealedProposal) error {
				return ip.Insert(*proposal)
			},
			isStuckFunc: func(num uint64) (uint64, bool) {
				return 0, false
			},
		})
	}

	return cluster, timeoutsChan
}

func runClusterCycle(cluster []*pbft.Pbft, _ int, stuckList, doneList *helper.BoolSlice) error {
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
			errCh := make(chan error)
			if stuckList.Get(i) {
				return nil
			}

			if doneList.Get(i) {
				return nil
			}

			go func() {
				defer func() {
					if r := recover(); r != nil {
						errCh <- fmt.Errorf("%v", r)
					}
				}()
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
			case er := <-errCh:
				err1 = er
			}

			// useful for debug
			_, _, _ = state, wgTime, isLocked
			// if time.Since(wgTime) > waitDuration {
			//	fmt.Println("wgitme ", state, i, callNumber, time.Since(wgTime), err1, isLocked)
			// }
			//
			return err1
		})
	}

	return wg.Wait()
}

func setDoneOnDoneState(cluster []*pbft.Pbft, doneList *helper.BoolSlice) {
	for i, node := range cluster {
		state := node.GetState()
		if state == pbft.DoneState {
			doneList.Set(i, true)
		}
	}
}

type Errorer interface {
	Error(args ...interface{})
}

func sendTimeoutIfNNodesStucked(t Errorer, timeoutsChan []chan time.Time, numOfNodes int) func(stuckList *helper.BoolSlice) bool {
	return func(stuckList *helper.BoolSlice) bool {
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
	handleStuckList func(*helper.BoolSlice) bool,
	handleDoneList func(*helper.BoolSlice) bool,
	handleMaxRoundNumber func(uint64) bool,
	limitCallNumber int,
) error {
	for i := range cluster {
		cluster[i].SetInitialState(context.Background())
	}

	stuckList := helper.NewBoolSlice(len(cluster))
	doneList := helper.NewBoolSlice(len(cluster))

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

// finalProposal struct contains inserted final proposals for the node
type finalProposal struct {
	lock sync.Mutex
	bc   map[uint64]pbft.Proposal
}

func (i *finalProposal) Insert(proposal pbft.SealedProposal) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	if p, ok := i.bc[proposal.Number]; ok {
		if !p.Equal(proposal.Proposal) {
			panic("wrong proposal inserted")
		}
	} else {
		i.bc[proposal.Number] = *proposal.Proposal
	}
	return nil
}
