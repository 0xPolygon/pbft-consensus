package ibft

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"reflect"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type Config struct {
}

type Proposal2 struct {
	Proposal       []byte
	CommittedSeals [][]byte
	Proposer       NodeID
	Number         uint64
}

type Interface interface {
	BuildBlock() (*Proposal, error)
	Validate(proposal []byte) ([]byte, error)
	Insert(p *Proposal2) error
	ValidatorSet() (*Snapshot, error)
	Hash(p []byte) ([]byte, error)
	IsStuck(num uint64) (uint64, bool)
}

type Snapshot struct {
	Number       uint64
	ValidatorSet ValidatorSetInterface
}

// Ibft represents the IBFT consensus mechanism object
type Ibft struct {
	inter          Interface
	logger         *log.Logger   // Output logger
	state          *currentState // Reference to the current state
	closeCh        chan struct{} // Channel for closing
	validator      SignKey
	msgQueue       *msgQueue     // Structure containing different message queues
	updateCh       chan struct{} // Update channel
	transport      Transport
	tracer         trace.Tracer
	forceTimeoutCh bool
}

type SignKey interface {
	NodeID() NodeID
	Sign(b []byte) ([]byte, error)
}

// Factory implements the base consensus Factory method
func Factory(logger *log.Logger /*, config *Config*/, inter Interface, validator SignKey, transport Transport) (*Ibft, error) {
	if logger == nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
	}

	p := &Ibft{
		inter:     inter,
		logger:    logger,
		closeCh:   make(chan struct{}),
		validator: validator,
		state:     &currentState{},
		transport: transport,
		msgQueue:  newMsgQueue(),
		updateCh:  make(chan struct{}),
	}

	p.logger.Printf("[INFO] validator key: addr=%s\n", p.validator.NodeID())
	return p, nil
}

func (i *Ibft) SetTrace(trace trace.Tracer) {
	i.tracer = trace
}

// start starts the IBFT consensus state machine
func (i *Ibft) Run(ctx context.Context) {
	// set the tracer to noop if nothing set
	if i.tracer == nil {
		i.tracer = trace.NewNoopTracerProvider().Tracer("")
	}

	// if we have arrive at this point we are assuming that we are synced
	// since this will assume we are good we have first to move to its initial
	// state for consensus that is the AcceptState
	i.setState(AcceptState)

	var sequenceCtx context.Context
	var span trace.Span
	var sequence uint64

	checkSpanChange := func() {
		currentSequence := i.state.GetSequence()
		if sequence == currentSequence && sequenceCtx != nil {
			return
		}
		if span != nil {
			// finish the current span
			span.End()
		}
		// create a new span
		sequence = currentSequence
		sequenceCtx, span = i.tracer.Start(context.Background(), fmt.Sprintf("Sequence-%d", sequence))
	}

	// first init the context and span
	checkSpanChange()

	for i.getState() != SyncState {
		select {
		case <-ctx.Done():
			return
		default: // Default is here because we would block until we receive something in the closeCh
		}

		// Start the state machine loop
		i.runCycle(sequenceCtx)

		//span.End()
		checkSpanChange()
	}
}

// runCycle represents the IBFT state machine loop
func (i *Ibft) runCycle(ctx context.Context) {
	// Log to the console
	if i.state.view != nil {
		i.logger.Printf("[DEBUG] cycle: state=%s, sequence=%d, round=%d", i.getState(), i.state.view.Sequence, i.state.view.Round)
	}

	// Based on the current state, execute the corresponding section
	switch i.getState() {
	case AcceptState:
		i.runAcceptState(ctx)

	case ValidateState:
		i.runValidateState(ctx)

	case RoundChangeState:
		i.runRoundChangeState(ctx)

	case CommitState:
		i.runCommitState(ctx)
	}
}

func (i *Ibft) SetSequence(sequence uint64) {
	i.state.view = &View{
		Round:    0,
		Sequence: sequence,
	}
}

// runAcceptState runs the Accept state loop
//
// The Accept state always checks the snapshot, and the validator set. If the current node is not in the validators set,
// it moves back to the Sync state. On the other hand, if the node is a validator, it calculates the proposer.
// If it turns out that the current node is the proposer, it builds a block, and sends preprepare and then prepare messages.
func (i *Ibft) runAcceptState(ctx context.Context) { // start new round
	_, span := i.tracer.Start(ctx, "AcceptState")
	defer span.End()

	i.logger.Printf("[INFO] accept state: sequence %d", i.state.view.Sequence)

	snap, err := i.inter.ValidatorSet()
	if err != nil {
		i.setState(SyncState)
		return
	}
	if !snap.ValidatorSet.Includes(i.validator.NodeID()) {
		// we are not a validator anymore, move back to sync state
		i.logger.Printf("[INFO] we are not a validator anymore")
		i.setState(SyncState)
		return
	}

	i.state.snap = snap
	i.state.validators = snap.ValidatorSet

	// reset round messages
	i.state.resetRoundMsgs()
	i.state.CalcProposer()

	isProposer := i.state.proposer == i.validator.NodeID()

	// log the current state of this span
	span.SetAttributes(
		attribute.Bool("isproposer", isProposer),
		attribute.Bool("locked", i.state.locked),
		attribute.String("proposer", string(i.state.proposer)),
	)

	if isProposer {
		i.logger.Printf("[INFO] we are the proposer")

		if !i.state.locked {
			// since the state is not locked, we need to build a new proposal
			i.state.proposal, err = i.inter.BuildBlock( /*snap, parent*/ )
			if err != nil {
				i.logger.Print("[ERROR] failed to build block", "err", err)
				i.setState(RoundChangeState)
				return
			}

			// calculate how much time do we have to wait to mine the block
			delay := time.Until(i.state.proposal.Time)

			select {
			case <-time.After(delay):
			case <-i.closeCh:
				return
			}

		}

		// send the preprepare message as an RLP encoded block
		i.sendPreprepareMsg()

		// send the prepare message since we are ready to move the state
		i.sendPrepareMsg()

		// move to validation state for new prepare messages
		i.setState(ValidateState)
		return
	}

	i.logger.Printf("[INFO] proposer calculated: proposer=%s, sequence=%d", i.state.proposer, snap.Number)

	// we are NOT a proposer for the block. Then, we have to wait
	// for a pre-prepare message from the proposer

	timeout := i.randomTimeout()

	// We only need to wait here for one type of message, the Prepare message from the proposer.
	// However, since we can receive bad Prepare messages we have to wait (or timeout) until
	// we get the message from the correct proposer.
	for i.getState() == AcceptState {
		msg, ok := i.getNextMessage(span, timeout)
		if !ok {
			return
		}
		if msg == nil {
			i.setState(RoundChangeState)
			continue
		}

		if msg.From != i.state.proposer {
			i.logger.Printf("[ERROR] msg received from wrong proposer: expected=%s, found=%s", i.state.proposer, msg.From)
			continue
		}

		// retrieve the block proposal
		if _, err := i.inter.Validate(msg.Proposal); err != nil {
			i.logger.Print("[ERROR] failed to unmarshal block", "err", err)
			i.setState(RoundChangeState)
			return
		}

		if i.state.locked {
			fmt.Println("Xxx")

			hash1, _ := i.inter.Hash(msg.Proposal)
			hash2, _ := i.inter.Hash(i.state.proposal.Data)

			// the state is locked, we need to receive the same block
			if bytes.Equal(hash1, hash2) {
				// fast-track and send a commit message and wait for validations
				i.sendCommitMsg()
				i.setState(ValidateState)
			} else {
				i.handleStateErr(errIncorrectLockedProposal)
			}
		} else {
			/*
				// since its a new block, we have to verify it first
				if err := i.verifyHeaderImpl(snap, parent, block.Header); err != nil {
					i.logger.Printf("[ERROR] block verification failed", "err", err)
					i.handleStateErr(errBlockVerificationFailed)
				} else {
			*/
			i.state.proposal = &Proposal{
				Data: msg.Proposal,
			}

			// send prepare message and wait for validations
			i.sendPrepareMsg()
			i.setState(ValidateState)
			//}
		}
	}
}

// runValidateState implements the Validate state loop.
//
// The Validate state is rather simple - all nodes do in this state is read messages and add them to their local snapshot state
func (i *Ibft) runValidateState(ctx context.Context) { // start new round
	ctx, span := i.tracer.Start(ctx, "ValidateState")
	defer span.End()

	hasCommitted := false
	sendCommit := func(span trace.Span) {
		// at this point either we have enough prepare messages
		// or commit messages so we can lock the block
		i.state.lock()

		if !hasCommitted {
			// send the commit message
			i.sendCommitMsg()
			hasCommitted = true

			span.AddEvent("Commit")
		}
	}

	timeout := i.randomTimeout()
	for i.getState() == ValidateState {
		_, span := i.tracer.Start(ctx, "ValidateState")

		msg, ok := i.getNextMessage(span, timeout)
		if !ok {
			// closing
			span.End()
			return
		}
		if msg == nil {
			// timeout
			i.setState(RoundChangeState)
			span.End()
			continue
		}

		switch msg.Type {
		case MessageReq_Prepare:
			i.state.addPrepared(msg)

		case MessageReq_Commit:
			i.state.addCommitted(msg)

		default:
			panic(fmt.Sprintf("BUG: %s", reflect.TypeOf(msg.Type)))
		}

		if i.state.numPrepared() > i.state.NumValid() {
			// we have received enough pre-prepare messages
			sendCommit(span)
		}

		if i.state.numCommitted() > i.state.NumValid() {
			// we have received enough commit messages
			sendCommit(span)

			// change to commit state just to get out of the loop
			i.setState(CommitState)
		}

		// set the attributes of this span once it is done
		i.setStateSpanAttributes(span)

		span.End()
	}
}

func spanAddEventMessage(typ string, span trace.Span, msg *MessageReq) {
	span.AddEvent("Message", trace.WithAttributes(
		// where was the message generated
		attribute.String("typ", typ),

		// type of message
		attribute.String("msg", msg.Type.String()),

		// from address of the sender
		attribute.String("from", string(msg.From)),

		// view sequence
		attribute.Int64("sequence", int64(msg.View.Sequence)),

		// round sequence
		attribute.Int64("round", int64(msg.View.Round)),
	))
}

func (i *Ibft) setStateSpanAttributes(span trace.Span) {
	attr := []attribute.KeyValue{}

	// number of committed messages
	attr = append(attr, attribute.Int64("committed", int64(i.state.numCommitted())))

	// number of prepared messages
	attr = append(attr, attribute.Int64("prepared", int64(i.state.numPrepared())))

	// number of change state messages per round
	for round, msgs := range i.state.roundMessages {
		attr = append(attr, attribute.Int64(fmt.Sprintf("roundchange_%d", round), int64(len(msgs))))
	}
	span.SetAttributes(attr...)
}

func (i *Ibft) runCommitState(ctx context.Context) {
	_, span := i.tracer.Start(ctx, "CommitState")
	defer span.End()

	committedSeals := i.state.getCommittedSeals()
	proposal := i.state.proposal.Data

	// at this point either if it works or not we need to unlock the state
	// to allow for other block to be produced if it insertion fails
	i.state.unlock()

	pp := &Proposal2{
		Proposal:       proposal,
		CommittedSeals: committedSeals,
		Proposer:       i.state.proposer,
		Number:         i.state.snap.Number,
	}
	if err := i.inter.Insert(pp); err != nil {
		// start a new round with the state unlocked since we need to
		// be able to propose/validate a different block
		i.logger.Print("[ERROR] failed to insert proposal", "err", err)
		i.handleStateErr(errFailedToInsertBlock)
	} else {
		i.SetSequence(i.state.snap.Number + 1)

		// move ahead to the next block
		i.setState(AcceptState)
	}
}

var (
	errIncorrectLockedProposal = fmt.Errorf("locked proposal is incorrect")
	errBlockVerificationFailed = fmt.Errorf("block verification failed")
	errFailedToInsertBlock     = fmt.Errorf("failed to insert block")
)

func (i *Ibft) handleStateErr(err error) {
	i.state.err = err
	i.setState(RoundChangeState)
}

func (i *Ibft) runRoundChangeState(ctx context.Context) {
	ctx, span := i.tracer.Start(ctx, "RoundChange")
	defer span.End()

	sendRoundChange := func(round uint64) {
		i.logger.Print("[DEBUG] local round change", "round", round)
		// set the new round
		i.state.view.Round = round
		// clean the round
		i.state.cleanRound(round)
		// send the round change message
		i.sendRoundChange()
	}
	sendNextRoundChange := func() {
		sendRoundChange(i.state.view.Round + 1)
	}

	checkTimeout := func() {
		// At this point we might be stuck in the network if:
		// - We have advanced the round but everyone else passed.
		//   We are removing those messages since they are old now.
		if bestHeight, stucked := i.inter.IsStuck(i.state.view.Sequence); stucked {
			span.AddEvent("OutOfSync", trace.WithAttributes(
				// our local height
				attribute.Int64("local", int64(i.state.view.Sequence)),
				// the best remote height
				attribute.Int64("remote", int64(bestHeight)),
			))
			i.setState(SyncState)
			return
		}

		// otherwise, it seems that we are in sync
		// and we should start a new round
		sendNextRoundChange()
	}

	// if the round was triggered due to an error, we send our own
	// next round change
	if err := i.state.getErr(); err != nil {
		i.logger.Print("[DEBUG] round change handle err", "err", err)
		sendNextRoundChange()
	} else {
		// otherwise, it is due to a timeout in any stage
		// First, we try to sync up with any max round already available
		if maxRound, ok := i.state.maxRound(); ok {
			i.logger.Print("[DEBUG] round change set max round", "round", maxRound)
			sendRoundChange(maxRound)
		} else {
			// otherwise, do your best to sync up
			checkTimeout()
		}
	}

	// create a timer for the round change
	timeout := i.randomTimeout()
	for i.getState() == RoundChangeState {
		_, span := i.tracer.Start(ctx, "RoundChangeState")

		msg, ok := i.getNextMessage(span, timeout)
		if !ok {
			// closing
			span.End()
			return
		}
		if msg == nil {
			i.logger.Printf("[DEBUG] round change timeout")
			checkTimeout()
			//update the timeout duration
			timeout = i.randomTimeout()
			span.End()
			continue
		}

		// we only expect RoundChange messages right now
		num := i.state.AddRoundMessage(msg)

		if num == i.state.NumValid() {
			// start a new round inmediatly
			i.state.view.Round = msg.View.Round
			i.setState(AcceptState)
		} else if num == i.state.MaxFaultyNodes()+1 {
			// weak certificate, try to catch up if our round number is smaller
			if i.state.view.Round < msg.View.Round {
				// update timer
				timeout = i.randomTimeout()
				sendRoundChange(msg.View.Round)
			}
		}

		i.setStateSpanAttributes(span)
		span.End()
	}
}

// --- communication wrappers ---

func (i *Ibft) sendRoundChange() {
	i.gossip(MessageReq_RoundChange)
}

func (i *Ibft) sendPreprepareMsg() {
	i.gossip(MessageReq_Preprepare)
}

func (i *Ibft) sendPrepareMsg() {
	i.gossip(MessageReq_Prepare)
}

func (i *Ibft) sendCommitMsg() {
	i.gossip(MessageReq_Commit)
}

func (i *Ibft) gossip(typ MsgType) {
	msg := &MessageReq{
		Type: typ,
		From: i.validator.NodeID(),
	}

	// add View
	msg.View = i.state.view.Copy()

	// if we are sending a preprepare message we need to include the proposed block
	if msg.Type == MessageReq_Preprepare {
		msg.SetProposal(i.state.proposal.Data)
	}

	// if the message is commit, we need to add the committed seal
	if msg.Type == MessageReq_Commit {
		// seal the hash of the proposal
		hash, _ := i.inter.Hash(i.state.proposal.Data)

		seal, err := i.validator.Sign(hash)
		if err != nil {
			i.logger.Print("[ERROR] failed to commit seal", "err", err)
			return
		}
		msg.Seal = seal
	}

	if msg.Type != MessageReq_Preprepare {
		// send a copy to ourselves so that we can process this message as well
		msg2 := msg.Copy()
		msg2.From = i.validator.NodeID()
		i.pushMessage(msg2)
	}
	if err := i.transport.Gossip(msg); err != nil {
		i.logger.Print("[ERROR] failed to gossip", "err", err)
	}
}

func (i *Ibft) GetState() IbftState {
	return i.getState()
}

// getState returns the current IBFT state
func (i *Ibft) getState() IbftState {
	return i.state.getState()
}

// isState checks if the node is in the passed in state
func (i *Ibft) IsState(s IbftState) bool {
	return i.state.getState() == s
}

func (i *Ibft) SetState(s IbftState) {
	i.setState(s)
}

// setState sets the IBFT state
func (i *Ibft) setState(s IbftState) {
	i.logger.Printf("[DEBUG] state change: '%s'", s)
	i.state.setState(s)
}

// forceTimeout sets the forceTimeoutCh flag to true
func (i *Ibft) forceTimeout() {
	i.forceTimeoutCh = true
}

// randomTimeout calculates the timeout duration depending on the current round
func (i *Ibft) randomTimeout() time.Duration {
	// timeout := time.Duration(2000) * time.Millisecond
	timeout := 2 * time.Second
	round := i.state.view.Round
	if round > 0 {
		timeout += time.Duration(math.Pow(2, float64(round))) * time.Second
	}
	return timeout
}

// Close closes the IBFT consensus mechanism, and does write back to disk
func (i *Ibft) Close() error {
	close(i.closeCh)
	return nil
}

// getNextMessage reads a new message from the message queue
func (i *Ibft) getNextMessage(span trace.Span, timeout time.Duration) (*MessageReq, bool) {
	timeoutCh := time.After(timeout)
	for {
		msg, discards := i.msgQueue.readMessageWithDiscards(i.getState(), i.state.view)
		// send the discard messages
		for _, msg := range discards {
			spanAddEventMessage("dropMessage", span, msg.obj)
		}
		if msg != nil {
			// add the event to the span
			spanAddEventMessage("message", span, msg.obj)

			return msg.obj, true
		}

		if i.forceTimeoutCh {
			i.forceTimeoutCh = false
			return nil, true
		}

		// wait until there is a new message or
		// someone closes the stopCh (i.e. timeout for round change)
		select {
		case <-timeoutCh:
			span.AddEvent("Timeout")
			return nil, true
		case <-i.closeCh:
			return nil, false
		case <-i.updateCh:
		}
	}
}

func (i *Ibft) PushMessage(msg *MessageReq) {
	i.pushMessage(msg)
}

// pushMessage pushes a new message to the message queue
func (i *Ibft) pushMessage(msg *MessageReq) {
	task := &msgTask{
		view: msg.View,
		msg:  msg.Type,
		obj:  msg,
	}
	i.msgQueue.pushMessage(task)

	select {
	case i.updateCh <- struct{}{}:
	default:
	}
}
