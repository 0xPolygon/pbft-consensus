package pbft

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMsgQueue_RoundChangeState(t *testing.T) {
	m := newMsgQueue()

	// insert non round change messages
	{
		m.pushMessage(createMessage("A", MessageReq_Prepare, ViewMsg(1, 1)))
		m.pushMessage(createMessage("B", MessageReq_Commit, ViewMsg(1, 1)))

		// we only read round state messages
		assert.Nil(t, m.readMessage(RoundChangeState, ViewMsg(1, 0)))
	}

	// insert old round change messages
	{
		m.pushMessage(createMessage("C", MessageReq_RoundChange, ViewMsg(1, 1)))

		// the round change message is old
		assert.Nil(t, m.readMessage(RoundChangeState, ViewMsg(2, 0)))
		assert.Zero(t, m.roundChangeStateQueue.Len())
	}

	// insert two valid round change messages with an old one in the middle
	{
		m.pushMessage(createMessage("D", MessageReq_RoundChange, ViewMsg(2, 2)))
		m.pushMessage(createMessage("E", MessageReq_RoundChange, ViewMsg(1, 1)))
		m.pushMessage(createMessage("F", MessageReq_RoundChange, ViewMsg(2, 1)))

		msg1 := m.readMessage(RoundChangeState, ViewMsg(2, 0))
		assert.NotNil(t, msg1)
		assert.Equal(t, msg1.From, NodeID("F"))

		msg2 := m.readMessage(RoundChangeState, ViewMsg(2, 0))
		assert.NotNil(t, msg2)
		assert.Equal(t, msg2.From, NodeID("D"))
	}

	// insert future messages to the queue => such messages should not be retrieved back
	{
		m = newMsgQueue()
		m.pushMessage(createMessage("A", MessageReq_RoundChange, ViewMsg(3, 1)))
		assert.Nil(t, m.readMessage(RoundChangeState, ViewMsg(1, 1)))

		m.pushMessage(createMessage("A", MessageReq_Commit, ViewMsg(3, 1)))
		assert.Nil(t, m.readMessage(CommitState, ViewMsg(1, 1)))
	}
}

func Test_msgToState(t *testing.T) {
	expectedResult := map[MsgType]State{
		MessageReq_RoundChange: RoundChangeState,
		MessageReq_Preprepare:  AcceptState,
		MessageReq_Prepare:     ValidateState,
		MessageReq_Commit:      ValidateState,
	}
	for msgType, st := range expectedResult {
		assert.Equal(t, st, msgToState(msgType))
	}
}

func TestCmpView(t *testing.T) {
	var cases = []struct {
		x, y           *View
		expectedResult int
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
		{
			&View{
				Sequence: 2,
				Round:    1,
			},
			&View{
				Sequence: 1,
				Round:    1,
			},
			1,
		},
		{
			&View{
				Sequence: 1,
				Round:    1,
			},
			&View{
				Sequence: 1,
				Round:    2,
			},
			-1,
		},
		{
			&View{
				Sequence: 1,
				Round:    2,
			},
			&View{
				Sequence: 1,
				Round:    1,
			},
			1,
		},
		{
			&View{
				Sequence: 1,
				Round:    1,
			},
			&View{
				Sequence: 1,
				Round:    1,
			},
			0,
		},
	}

	for _, c := range cases {
		assert.Equal(t, cmpView(c.x, c.y), c.expectedResult)
	}
}
