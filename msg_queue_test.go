package ibft

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func mockQueueMsg(id string, state MsgType, view *View) *msgTask {
	return &msgTask{
		view: view,
		msg:  state,
		obj: &MessageReq{
			// use the from field to identify the msg
			From: NodeID(id),
		},
	}
}

func TestMsgQueue_RoundChangeState(t *testing.T) {
	m := newMsgQueue()

	// insert non round change messages
	{
		m.pushMessage(mockQueueMsg("A", MessageReq_Prepare, ViewMsg(1, 0)))
		m.pushMessage(mockQueueMsg("B", MessageReq_Commit, ViewMsg(1, 0)))

		// we only read round state messages
		assert.Nil(t, m.readMessage(RoundChangeState, ViewMsg(1, 0)))
	}

	// insert old round change messages
	{
		m.pushMessage(mockQueueMsg("C", MessageReq_RoundChange, ViewMsg(1, 1)))

		// the round change message is old
		assert.Nil(t, m.readMessage(RoundChangeState, ViewMsg(2, 0)))
		assert.Zero(t, m.roundChangeStateQueue.Len())
	}

	// insert two valid round change messages with an old one in the middle
	{
		m.pushMessage(mockQueueMsg("D", MessageReq_RoundChange, ViewMsg(2, 2)))
		m.pushMessage(mockQueueMsg("E", MessageReq_RoundChange, ViewMsg(1, 1)))
		m.pushMessage(mockQueueMsg("F", MessageReq_RoundChange, ViewMsg(2, 1)))

		msg1 := m.readMessage(RoundChangeState, ViewMsg(2, 0))
		assert.NotNil(t, msg1)
		assert.Equal(t, msg1.obj.From, "F")

		msg2 := m.readMessage(RoundChangeState, ViewMsg(2, 0))
		assert.NotNil(t, msg2)
		assert.Equal(t, msg2.obj.From, "D")
	}
}

func TestCmpView(t *testing.T) {
	var cases = []struct {
		v, y *View
		res  int
	}{
		{
			&View{
				Sequence: 1,
				Round:    1,
			},
			&View{
				Sequence: 2,
				Round:    1,
			},
			-1,
		},
	}

	for _, c := range cases {
		assert.Equal(t, cmpView(c.v, c.y), c.res)
	}
}
