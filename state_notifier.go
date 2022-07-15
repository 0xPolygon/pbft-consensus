package pbft

// StateNotifier enables custom logic encapsulation related to internal triggers within PBFT state machine (namely receiving timeouts).
type StateNotifier interface {
	// HandleTimeout notifies that a timeout occurred while getting next message
	HandleTimeout(to NodeID, msgType MsgType, view *View)

	// ReadNextMessage reads the next message from message queue of the state machine
	ReadNextMessage(p *Pbft) (*MessageReq, []*MessageReq)
}

// DefaultStateNotifier is a null object implementation of StateNotifier interface
type DefaultStateNotifier struct {
}

// HandleTimeout implements StateNotifier interface
func (d *DefaultStateNotifier) HandleTimeout(NodeID, MsgType, *View) {}

// ReadNextMessage is an implementation of StateNotifier interface
func (d *DefaultStateNotifier) ReadNextMessage(p *Pbft) (*MessageReq, []*MessageReq) {
	return p.ReadMessageWithDiscards()
}
