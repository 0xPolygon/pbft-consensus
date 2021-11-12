package ibft

import (
	"container/heap"
	"sync"
)

// msgQueue defines the structure that holds message queues for different IBFT states
type msgQueue struct {
	// Heap implementation for the round change message queue
	roundChangeStateQueue msgQueueImpl

	// Heap implementation for the accept state message queue
	acceptStateQueue msgQueueImpl

	// Heap implementation for the validate state message queue
	validateStateQueue msgQueueImpl

	queueLock sync.Mutex
}

// pushMessage adds a new message to a message queue
func (m *msgQueue) pushMessage(task *msgTask) {
	m.queueLock.Lock()

	queue := m.getQueue(msgToState(task.msg))
	heap.Push(queue, task)

	m.queueLock.Unlock()
}

// readMessage reads the message from a message queue, based on the current state and view
func (m *msgQueue) readMessage(state IbftState, current *View) *msgTask {
	msg, _ := m.readMessageWithDiscards(state, current)
	return msg
}

func (m *msgQueue) readMessageWithDiscards(state IbftState, current *View) (*msgTask, []*msgTask) {
	m.queueLock.Lock()
	defer m.queueLock.Unlock()

	discarded := []*msgTask{}
	queue := m.getQueue(state)

	for {
		if queue.Len() == 0 {
			return nil, discarded
		}
		msg := queue.head()

		// check if the message is from the future
		if state == RoundChangeState {
			// if we are in RoundChangeState we only care about sequence
			// since we are interested in knowing all the possible rounds
			if msg.view.Sequence > current.Sequence {
				// future message
				return nil, discarded
			}
		} else {
			// otherwise, we compare both sequence and round
			if cmpView(msg.view, current) > 0 {
				// future message
				return nil, discarded
			}
		}

		// at this point, 'msg' is good or old, in either case
		// we have to remove it from the queue
		heap.Pop(queue)

		if cmpView(msg.view, current) < 0 {
			// old value, try again
			discarded = append(discarded, msg)
			continue
		}

		// good value, return it
		return msg, discarded
	}
}

// getQueue checks the passed in state, and returns the corresponding message queue
func (m *msgQueue) getQueue(state IbftState) *msgQueueImpl {
	if state == RoundChangeState {
		// round change
		return &m.roundChangeStateQueue
	} else if state == AcceptState {
		// preprepare
		return &m.acceptStateQueue
	} else {
		// prepare and commit
		return &m.validateStateQueue
	}
}

// newMsgQueue creates a new message queue structure
func newMsgQueue() *msgQueue {
	return &msgQueue{
		roundChangeStateQueue: msgQueueImpl{},
		acceptStateQueue:      msgQueueImpl{},
		validateStateQueue:    msgQueueImpl{},
	}
}

// msgToState converts the message type to an IbftState
func msgToState(msg MsgType) IbftState {
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

type msgTask struct {
	// priority
	view *View
	msg  MsgType

	obj *MessageReq
}

type msgQueueImpl []*msgTask

// head returns the head of the queue
func (m msgQueueImpl) head() *msgTask {
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
	if ti.view.Sequence != tj.view.Sequence {
		return ti.view.Sequence < tj.view.Sequence
	}
	// sort by round
	if ti.view.Round != tj.view.Round {
		return ti.view.Round < tj.view.Round
	}
	// sort by message
	return ti.msg < tj.msg
}

// Swap swaps the places of the items at the passed-in indexes
func (m msgQueueImpl) Swap(i, j int) {
	m[i], m[j] = m[j], m[i]
}

// Push adds a new item to the queue
func (m *msgQueueImpl) Push(x interface{}) {
	*m = append(*m, x.(*msgTask))
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

// cmpView compares two proto views.
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
