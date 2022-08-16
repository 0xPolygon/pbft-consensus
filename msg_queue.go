package pbft

import (
	"container/heap"
	"math"
	"sync"
)

// msgQueue defines the structure that holds message queues for different PBFT states
type msgQueue struct {
	// roundChangeStateQueue is a heap implementation for the round change message queue
	roundChangeStateQueue *msgQueueImpl

	// acceptStateQueue is a heap implementation for the accept state message queue
	acceptStateQueue *msgQueueImpl

	// validateStateQueue is a heap implementation for the validate state message queue
	validateStateQueue *msgQueueImpl

	// notifyMessageCh is a channel used to notify when a new message is pushed to the message queue
	notifyMessageCh chan struct{}

	// queueLock ensures thread-safety for mutating and reading each message queue
	queueLock sync.RWMutex

	// commitValidation contains logic for concurrent validation of commit messages
	commitValidation *commitValidationRoutine
}

// pushMessage adds a new message to a message queue
func (m *msgQueue) pushMessage(message *MessageReq) {
	m.queueLock.Lock()
	defer m.queueLock.Unlock()

	queue := m.getQueue(msgToState(message.Type))
	heap.Push(queue, message)

	select {
	case m.notifyMessageCh <- struct{}{}:
	default:
	}
}

// pushCommitMessage adds a new message to a pending commit messages queue
func (m *msgQueue) pushCommitMessage(msg *MessageReq) {
	m.commitValidation.pushPendingCommitMessage(msg)
}

// readMessage reads the message from a message queue, based on the current state and view
func (m *msgQueue) readMessage(st State, current *View) *MessageReq {
	msg, _ := m.readMessageWithDiscards(st, current)
	return msg
}

func (m *msgQueue) readMessageWithDiscards(st State, current *View) (*MessageReq, []*MessageReq) {
	m.queueLock.RLock()
	defer m.queueLock.RUnlock()

	queue := m.getQueue(st)
	return queue.readMessageWithDiscardsLocked(current, st)
}

// getQueue checks the passed in state, and returns the corresponding message queue
func (m *msgQueue) getQueue(st State) *msgQueueImpl {
	if st == RoundChangeState {
		// round change
		return m.roundChangeStateQueue
	} else if st == AcceptState {
		// preprepare
		return m.acceptStateQueue
	} else {
		// prepare and commit
		return m.validateStateQueue
	}
}

// newMsgQueue creates a new message queue structure
func newMsgQueue() *msgQueue {
	m := &msgQueue{
		roundChangeStateQueue: &msgQueueImpl{},
		acceptStateQueue:      &msgQueueImpl{},
		validateStateQueue:    &msgQueueImpl{},
		notifyMessageCh:       make(chan struct{}, 1), //hack: there is a bug when you have several messages pushed on the same time.
	}
	return m
}

// initCommitValidationRoutine initializes and starts commit validation routine
// which validates commit messages
func (m *msgQueue) initCommitValidationRoutine(logger Logger) {
	m.commitValidation = newCommitValidationRoutine(logger, m.pushMessage)
	go m.commitValidation.run()
}

type stateInfo struct {
	proposalHash []byte
	validators   ValidatorSet
	view         *View
}

// updateStateInfo receives data needed for commit message validation
// and triggers reading of commit messages
func (m *msgQueue) updateStateInfo(info *stateInfo) {
	// feed commit validation routine with necessary data
	m.commitValidation.updateStateInfoCh <- info
}

const (
	maxWorkersCount          = 5
	pendingCommitsBufferSize = 10
)

// commitValidationRoutine encapsulates concurrent commit messages validation logic.
// Valid commit messages are pushed to the validateStateQueue for further consumption by consensus algorithm.
// Invalid commit messages are discarded.
type commitValidationRoutine struct {
	pendingCommitMsgs     *msgQueueImpl
	lock                  sync.RWMutex
	logger                Logger
	validMsgHandler       func(msg *MessageReq)
	updateStateInfoCh     chan *stateInfo
	notifyPendingCommitCh chan struct{}
	closeCh               chan struct{}
}

// newCommitValidationRoutine creates a new instance of commitValidationRoutine object
func newCommitValidationRoutine(logger Logger, validMsgHandler func(msg *MessageReq)) *commitValidationRoutine {
	return &commitValidationRoutine{
		pendingCommitMsgs:     &msgQueueImpl{},
		validMsgHandler:       validMsgHandler,
		logger:                logger,
		updateStateInfoCh:     make(chan *stateInfo),
		notifyPendingCommitCh: make(chan struct{}),
		closeCh:               make(chan struct{}),
	}
}

// run contains main part of the commit messages validation logic
func (c *commitValidationRoutine) run() {
	// validateMsgWorker validates single commit message
	validateMsgWorker := func(commitMsgsCh <-chan *MessageReq, stateInfo *stateInfo) {
		for commitMsg := range commitMsgsCh {
			if err := stateInfo.validators.VerifySeal(commitMsg.From, commitMsg.Seal, stateInfo.proposalHash); err != nil {
				// TODO: Report open telemetry tracing for invalid commit messages?
				c.logger.Printf("[ERROR] Commit message is invalid (%v). Error: %s", commitMsg, err)
				return
			}
			c.validMsgHandler(commitMsg)
		}
	}

	var stateInfo *stateInfo
	for {
		commitMsgsCh := make(chan *MessageReq, pendingCommitsBufferSize)
		// wait for:
		// 1. new messages on the queue
		// 2. update of the round
		// 3. close message
		select {
		case <-c.notifyPendingCommitCh:
			if stateInfo == nil {
				continue
			}
		case info := <-c.updateStateInfoCh:
			stateInfo = info
			close(commitMsgsCh)
			commitMsgsCh = make(chan *MessageReq, pendingCommitsBufferSize)
		case <-c.closeCh:
			return
		}

		c.lock.RLock()
		workersNum := int(math.Min(float64(maxWorkersCount), float64(c.pendingCommitMsgs.Len())))
		c.lock.RUnlock()
		for i := 0; i < workersNum; i++ {
			go validateMsgWorker(commitMsgsCh, stateInfo)
		}

		for {
			c.lock.RLock()
			// read as many messages as possible for the current view
			commitMsg, _ := c.pendingCommitMsgs.readMessageWithDiscardsLocked(stateInfo.view, ValidateState)
			c.lock.RUnlock()
			if commitMsg == nil {
				break
			}
			commitMsgsCh <- commitMsg
		}
	}
}

func (c *commitValidationRoutine) close() {
	close(c.closeCh)
}

// pushPendingCommitMessage adds new commit message to the pending commit messages queue
func (c *commitValidationRoutine) pushPendingCommitMessage(msg *MessageReq) {
	c.lock.Lock()
	defer c.lock.Unlock()
	heap.Push(c.pendingCommitMsgs, msg)

	select {
	case c.notifyPendingCommitCh <- struct{}{}:
	default:
	}
}

// msgToState converts the message type to an State
func msgToState(msg MsgType) State {
	if msg == MessageReq_RoundChange {
		// round change
		return RoundChangeState
	} else if msg == MessageReq_Preprepare {
		// preprepare
		return AcceptState
	} else if msg == MessageReq_Prepare || msg == MessageReq_Commit {
		// prepare and commit
		return ValidateState
	}

	panic("BUG: not expected")
}

func stateToMsg(st State) MsgType {
	switch st {
	case RoundChangeState:
		return MessageReq_RoundChange
	case AcceptState:
		return MessageReq_Preprepare
	case ValidateState:
		return MessageReq_Prepare
	default:
		panic("BUG: not expected")
	}
}

type msgQueueImpl []*MessageReq

// readMessageWithDiscardsLocked reads message for the given round and sequence
// and discards messages from the previous rounds and sequences
func (m *msgQueueImpl) readMessageWithDiscardsLocked(view *View, pbftState State) (*MessageReq, []*MessageReq) {
	discarded := []*MessageReq{}
	for {
		if m.Len() == 0 {
			return nil, discarded
		}
		msg := m.head()

		// check if the message is from the future
		if pbftState == RoundChangeState {
			// if we are in RoundChangeState we only care about sequence
			// since we are interested in knowing all the possible rounds
			if msg.View.Sequence > view.Sequence {
				// future message
				return nil, discarded
			}
		} else {
			// otherwise, we compare both sequence and round
			if cmpView(msg.View, view) > 0 {
				// future message
				return nil, discarded
			}
		}

		// at this point, 'msg' is good or old, in either case
		// we have to remove it from the queue
		heap.Pop(m)

		if cmpView(msg.View, view) < 0 {
			// old value, try again
			discarded = append(discarded, msg)
			continue
		}

		// good value, return it
		return msg, discarded
	}
}

// head returns the head of the queue
func (m msgQueueImpl) head() *MessageReq {
	return m[0]
}

// Len returns the length of the queue
func (m msgQueueImpl) Len() int {
	return len(m)
}

// Less compares the priorities of two items at the passed in indexes (A < B)
func (m msgQueueImpl) Less(i, j int) bool {
	ti, tj := m[i], m[j]
	// sort by sequence
	if ti.View.Sequence != tj.View.Sequence {
		return ti.View.Sequence < tj.View.Sequence
	}
	// sort by round
	if ti.View.Round != tj.View.Round {
		return ti.View.Round < tj.View.Round
	}
	// sort by message
	return ti.Type < tj.Type
}

// Swap swaps the places of the items at the passed-in indexes
func (m msgQueueImpl) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

// Push adds a new item to the queue
func (m *msgQueueImpl) Push(x interface{}) {
	*m = append(*m, x.(*MessageReq))
}

// Pop removes an item from the queue
func (m *msgQueueImpl) Pop() interface{} {
	old := *m
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	*m = old[0 : n-1]
	return item
}

// cmpView compares two View objects.
//
// If v.Sequence == y.Sequence && v.Round == y.Round => 0
//
// If v.Sequence < y.Sequence => -1 ELSE => 1
//
// If v.Round < y.Round => -1 ELSE 1
func cmpView(v, y *View) int {
	if v.Sequence != y.Sequence {
		if v.Sequence < y.Sequence {
			return -1
		} else {
			return 1
		}
	}
	if v.Round != y.Round {
		if v.Round < y.Round {
			return -1
		} else {
			return 1
		}
	}

	return 0
}
