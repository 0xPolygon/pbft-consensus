package pbft

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/0xPolygon/pbft-consensus/stats"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type RoundTimeout func(round uint64) <-chan time.Time

type Config struct {
	// ProposalTimeout is the time to wait for the proposal
	// from the validator. It defaults to Timeout
	ProposalTimeout time.Duration

	// Timeout is the time to wait for validation and
	// round change messages
	Timeout time.Duration

	// Logger is the logger to output info
	Logger *log.Logger

	// Tracer is the OpenTelemetry tracer to log traces
	Tracer trace.Tracer

	// RoundTimeout is a function that calculates timeout based on a round number
	RoundTimeout RoundTimeout

	// Notifier is a reference to the struct which encapsulates handling messages and timeouts
	Notifier StateNotifier

	StatsCallback StatsCallback

	VotingPower map[NodeID]uint64
}

type ConfigOption func(*Config)

type StatsCallback func(stats.Stats)

func WithStats(s StatsCallback) ConfigOption {
	return func(c *Config) {
		c.StatsCallback = s
	}
}

func WithTimeout(p time.Duration) ConfigOption {
	return func(c *Config) {
		c.Timeout = p
	}
}

func WithProposalTimeout(p time.Duration) ConfigOption {
	return func(c *Config) {
		c.ProposalTimeout = p
	}
}

func WithLogger(l *log.Logger) ConfigOption {
	return func(c *Config) {
		c.Logger = l
	}
}

func WithTracer(t trace.Tracer) ConfigOption {
	return func(c *Config) {
		c.Tracer = t
	}
}

func WithRoundTimeout(roundTimeout RoundTimeout) ConfigOption {
	return func(c *Config) {
		if roundTimeout != nil {
			c.RoundTimeout = roundTimeout
		}
	}
}

func WithNotifier(notifier StateNotifier) ConfigOption {
	return func(c *Config) {
		if notifier != nil {
			c.Notifier = notifier
		}
	}
}

func WithVotingPower(vp map[NodeID]uint64) ConfigOption {
	return func(c *Config) {
		if len(vp) > 0 {
			c.VotingPower = vp
		}
	}
}

const (
	defaultTimeout     = 2 * time.Second
	maxTimeout         = 300 * time.Second
	maxTimeoutExponent = 8
)

func DefaultConfig() *Config {
	return &Config{
		Timeout:         defaultTimeout,
		ProposalTimeout: defaultTimeout,
		Logger:          log.New(os.Stderr, "", log.LstdFlags),
		Tracer:          trace.NewNoopTracerProvider().Tracer(""),
		RoundTimeout:    exponentialTimeout,
		Notifier:        &DefaultStateNotifier{},
	}
}

func (c *Config) ApplyOps(opts ...ConfigOption) {
	for _, opt := range opts {
		opt(c)
	}
}

type SealedProposal struct {
	Proposal       *Proposal
	CommittedSeals []CommittedSeal
	Proposer       NodeID
	Number         uint64
}

type Backend interface {
	// BuildProposal builds a proposal for the current round (used if proposer)
	BuildProposal() (*Proposal, error)

	// Validate validates a raw proposal (used if non-proposer)
	Validate(*Proposal) error

	// Insert inserts the sealed proposal
	Insert(p *SealedProposal) error

	// Height returns the height for the current round
	Height() uint64

	// ValidatorSet returns the validator set for the current round
	ValidatorSet() ValidatorSet

	// Init is used to signal the backend that a new round is going to start.
	Init(*RoundInfo)

	// IsStuck returns whether the pbft is stucked
	IsStuck(num uint64) (uint64, bool)

	// ValidateCommit is used to validate that a given commit is valid
	ValidateCommit(from NodeID, seal []byte) error
}

// RoundInfo is the information about the round
type RoundInfo struct {
	IsProposer bool
	Proposer   NodeID
	Locked     bool
}

// Pbft represents the PBFT consensus mechanism object
type Pbft struct {
	// Output logger
	logger *log.Logger

	// Config is the configuration of the consensus
	config *Config

	// inter is the interface with the runtime
	backend Backend

	// state is the reference to the current state machine
	state *currentState

	// validator is the signing key for this instance
	validator SignKey

	// ctx is the current execution context for an pbft round
	ctx context.Context

	// msgQueue is a queue that stores all the incomming gossip messages
	msgQueue *msgQueue

	// updateCh is a channel used to notify when a new gossip message arrives
	updateCh chan struct{}

	// Transport is the interface for the gossip transport
	transport Transport

	// tracer is a reference to the OpenTelemetry tracer
	tracer trace.Tracer

	// roundTimeout calculates timeout for a specific round
	roundTimeout RoundTimeout

	// notifier is a reference to the struct which encapsulates handling messages and timeouts
	notifier StateNotifier

	stats *stats.Stats
}

type SignKey interface {
	NodeID() NodeID
	Sign(b []byte) ([]byte, error)
}

// New creates a new instance of the PBFT state machine
func New(validator SignKey, transport Transport, opts ...ConfigOption) *Pbft {
	config := DefaultConfig()
	config.ApplyOps(opts...)

	p := &Pbft{
		validator:    validator,
		state:        newState(),
		transport:    transport,
		msgQueue:     newMsgQueue(),
		updateCh:     make(chan struct{}, 1), //hack. There is a bug when you have several messages pushed on the same time.
		config:       config,
		logger:       config.Logger,
		tracer:       config.Tracer,
		roundTimeout: config.RoundTimeout,
		notifier:     config.Notifier,
		stats:        stats.NewStats(),
	}

	p.logger.Printf("[INFO] validator key: addr=%s\n", p.validator.NodeID())
	return p
}

func (p *Pbft) SetBackend(backend Backend) error {
	p.backend = backend

	// set the next current sequence for this iteration
	p.setSequence(p.backend.Height())

	// set the current set of validators
	p.state.validators = p.backend.ValidatorSet()

	return nil
}

// start starts the PBFT consensus state machine
func (p *Pbft) Run(ctx context.Context) {
	p.SetInitialState(ctx)

	// start the trace span
	spanCtx, span := p.tracer.Start(context.Background(), fmt.Sprintf("Sequence-%d", p.state.view.Sequence))
	defer span.End()

	// loop until we reach the a finish state
	for p.getState() != DoneState && p.getState() != SyncState {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Start the state machine loop
		p.runCycle(spanCtx)

		// emit stats when the round is ended
		p.emitStats()
	}
}

func (p *Pbft) emitStats() {
	if p.config.StatsCallback != nil {
		p.config.StatsCallback(p.stats.Snapshot())
		// once emitted, reset the Stats
		p.stats.Reset()
	}
}

func (p *Pbft) SetInitialState(ctx context.Context) {
	p.ctx = ctx

	// the iteration always starts with the AcceptState.
	// AcceptState stages will reset the rest of the message queues.
	p.setState(AcceptState)
}
func (p *Pbft) RunCycle(ctx context.Context) {
	p.runCycle(ctx)
}

// runCycle represents the PBFT state machine loop
func (p *Pbft) runCycle(ctx context.Context) {
	startTime := time.Now()
	state := p.getState()
	defer p.stats.StateDuration(state.String(), startTime)

	// Log to the console
	if p.state.view != nil {
		p.logger.Printf("[DEBUG] cycle: state=%s, sequence=%d, round=%d", p.getState(), p.state.view.Sequence, p.state.GetCurrentRound())
	}
	// Based on the current state, execute the corresponding section
	switch state {
	case AcceptState:
		p.runAcceptState(ctx)

	case ValidateState:
		p.runValidateState(ctx)

	case RoundChangeState:
		p.runRoundChangeState(ctx)

	case CommitState:
		p.runCommitState(ctx)

	case DoneState:
		panic("BUG: We cannot iterate on DoneState")
	}
}

func (p *Pbft) setSequence(sequence uint64) {
	p.state.view = &View{
		Sequence: sequence,
	}
	p.setRound(0)
}

func (p *Pbft) setRound(round uint64) {
	p.state.SetCurrentRound(round)

	// reset current timeout and start a new one
	p.state.timeoutChan = p.roundTimeout(round)
}

// runAcceptState runs the Accept state loop
//
// The Accept state always checks the snapshot, and the validator set. If the current node is not in the validators set,
// it moves back to the Sync state. On the other hand, if the node is a validator, it calculates the proposer.
// If it turns out that the current node is the proposer, it builds a proposal, and sends preprepare and then prepare messages.
func (p *Pbft) runAcceptState(ctx context.Context) { // start new round
	_, span := p.tracer.Start(ctx, "AcceptState")
	defer span.End()

	p.stats.SetView(p.state.view.Sequence, p.state.view.Round)
	p.logger.Printf("[INFO] accept state: sequence %d", p.state.view.Sequence)

	if !p.state.validators.Includes(p.validator.NodeID()) {
		// we are not a validator anymore, move back to sync state
		p.logger.Print("[INFO] we are not a validator anymore")
		p.setState(SyncState)
		return
	}

	// reset round messages
	p.state.resetRoundMsgs()
	p.state.CalcProposer()

	isProposer := p.state.proposer == p.validator.NodeID()
	p.backend.Init(&RoundInfo{
		Proposer:   p.state.proposer,
		IsProposer: isProposer,
		Locked:     p.state.IsLocked(),
	})

	// log the current state of this span
	span.SetAttributes(
		attribute.Bool("isproposer", isProposer),
		attribute.Bool("locked", p.state.IsLocked()),
		attribute.String("proposer", string(p.state.proposer)),
	)

	var err error

	if isProposer {
		p.logger.Printf("[INFO] we are the proposer")

		if !p.state.IsLocked() {
			// since the state is not locked, we need to build a new proposal
			p.state.proposal, err = p.backend.BuildProposal()
			if err != nil {
				p.logger.Printf("[ERROR] failed to build proposal: %v", err)
				p.setState(RoundChangeState)
				return
			}

			// calculate how much time do we have to wait to gossip the proposal
			delay := time.Until(p.state.proposal.Time)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}

		// send the preprepare message
		p.sendPreprepareMsg()

		// send the prepare message since we are ready to move the state
		p.sendPrepareMsg()

		// move to validation state for new prepare messages
		p.setState(ValidateState)
		return
	}

	p.logger.Printf("[INFO] proposer calculated: proposer=%s, sequence=%d", p.state.proposer, p.state.view.Sequence)

	// we are NOT a proposer for this height/round. Then, we have to wait
	// for a pre-prepare message from the proposer

	// We only need to wait here for one type of message, the Prepare message from the proposer.
	// However, since we can receive bad Prepare messages we have to wait (or timeout) until
	// we get the message from the correct proposer.
	for p.getState() == AcceptState {
		msg, ok := p.getNextMessage(span)
		if !ok {
			return
		}
		if msg == nil {
			p.setState(RoundChangeState)
			continue
		}
		// TODO: Validate that the fields required for Preprepare are set (Proposal and Hash)
		if msg.From != p.state.proposer {
			p.logger.Printf("[ERROR] msg received from wrong proposer: expected=%s, found=%s", p.state.proposer, msg.From)
			continue
		}

		// retrieve the proposal, the backend MUST validate that the hash belongs to the proposal
		proposal := &Proposal{
			Data: msg.Proposal,
			Hash: msg.Hash,
		}
		if err := p.backend.Validate(proposal); err != nil {
			p.logger.Printf("[ERROR] failed to validate proposal. Error message: %v", err)
			p.setState(RoundChangeState)
			return
		}

		if p.state.IsLocked() {
			// the state is locked, we need to receive the same proposal
			if p.state.proposal.Equal(proposal) {
				// fast-track and send a commit message and wait for validations
				p.sendCommitMsg()
				p.setState(ValidateState)
			} else {
				p.handleStateErr(errIncorrectLockedProposal)
			}
		} else {
			p.state.proposal = proposal
			p.sendPrepareMsg()
			p.setState(ValidateState)
		}
	}
}

// runValidateState implements the Validate state loop.
//
// The Validate state is rather simple - all nodes do in this state is read messages and add them to their local snapshot state
func (p *Pbft) runValidateState(ctx context.Context) { // start new round
	ctx, span := p.tracer.Start(ctx, "ValidateState")
	defer span.End()

	hasCommitted := false
	sendCommit := func(span trace.Span) {
		// at this point either we have enough prepare messages
		// or commit messages so we can lock the proposal
		p.state.lock()

		if !hasCommitted {
			// send the commit message
			p.sendCommitMsg()
			hasCommitted = true

			span.AddEvent("Commit")
		}
	}

	for p.getState() == ValidateState {
		_, span := p.tracer.Start(ctx, "ValidateState")

		msg, ok := p.getNextMessage(span)
		if !ok {
			// closing
			span.End()
			return
		}
		if msg == nil {
			// timeout
			p.setState(RoundChangeState)
			span.End()
			return
		}

		// the message must have our local hash
		if !bytes.Equal(msg.Hash, p.state.proposal.Hash) {
			p.logger.Printf(fmt.Sprintf("[WARN]: incorrect hash in %s message", msg.Type.String()))
			continue
		}

		switch msg.Type {
		case MessageReq_Prepare:
			p.state.addPrepared(msg)

		case MessageReq_Commit:
			if err := p.backend.ValidateCommit(msg.From, msg.Seal); err != nil {
				p.logger.Printf("[ERROR]: failed to validate commit: %v", err)
				continue
			}
			p.state.addCommitted(msg)
		default:
			panic(fmt.Errorf("BUG: Unexpected message type: %s in %s", msg.Type, p.getState()))
		}

		if p.config.VotingPower == nil {
			if p.state.numPrepared() > p.state.NumValid() {
				// we have received enough prepare messages
				sendCommit(span)
			}

			if p.state.numCommitted() > p.state.NumValid() {
				// we have received enough commit messages
				sendCommit(span)

				// change to commit state just to get out of the loop
				p.setState(CommitState)
			}
		} else {
			totalVotingPower := totalVotingPower(p.config.VotingPower)
			validVotingPower := QuorumSizeVP(totalVotingPower)

			if p.state.calculateMessagesVotingPower(p.state.prepared, p.config.VotingPower) >= validVotingPower {
				// we have received enough prepare messages
				sendCommit(span)
			}

			if p.state.calculateMessagesVotingPower(p.state.committed, p.config.VotingPower) >= validVotingPower {
				// we have received enough commit messages
				sendCommit(span)

				// change to commit state just to get out of the loop
				p.setState(CommitState)
			}
		}

		// set the attributes of this span once it is done
		p.setStateSpanAttributes(span)

		span.End()
	}
}

func (p *Pbft) spanAddEventMessage(typ string, span trace.Span, msg *MessageReq) {
	p.stats.IncrMsgCount(msg.Type.String())

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

func (p *Pbft) setStateSpanAttributes(span trace.Span) {
	attr := []attribute.KeyValue{}

	// number of committed messages
	attr = append(attr, attribute.Int64("committed", int64(p.state.numCommitted())))

	// number of prepared messages
	attr = append(attr, attribute.Int64("prepared", int64(p.state.numPrepared())))

	// number of change state messages per round
	for round, msgs := range p.state.roundMessages {
		attr = append(attr, attribute.Int64(fmt.Sprintf("roundchange_%d", round), int64(len(msgs))))
	}
	span.SetAttributes(attr...)
}

func (p *Pbft) runCommitState(ctx context.Context) {
	_, span := p.tracer.Start(ctx, "CommitState")
	defer span.End()

	committedSeals := p.state.getCommittedSeals()
	proposal := p.state.proposal.Copy()

	// at this point either if it works or not we need to unlock the state
	// to allow for other proposals to be produced if it insertion fails
	p.state.unlock()

	pp := &SealedProposal{
		Proposal:       proposal,
		CommittedSeals: committedSeals,
		Proposer:       p.state.proposer,
		Number:         p.state.view.Sequence,
	}
	if err := p.backend.Insert(pp); err != nil {
		// start a new round with the state unlocked since we need to
		// be able to propose/validate a different proposal
		p.logger.Printf("[ERROR] failed to insert proposal. Error message: %v", err)
		p.handleStateErr(errFailedToInsertProposal)
	} else {
		// move to done state to finish the current iteration of the state machine
		p.setState(DoneState)
	}
}

var (
	errIncorrectLockedProposal = fmt.Errorf("locked proposal is incorrect")
	errVerificationFailed      = fmt.Errorf("proposal verification failed")
	errFailedToInsertProposal  = fmt.Errorf("failed to insert proposal")
)

func (p *Pbft) handleStateErr(err error) {
	p.state.err = err
	p.setState(RoundChangeState)
}

func (p *Pbft) runRoundChangeState(ctx context.Context) {
	ctx, span := p.tracer.Start(ctx, "RoundChange")
	defer span.End()

	sendRoundChange := func(round uint64) {
		p.logger.Printf("[DEBUG] local round change: round=%d", round)
		// set the new round
		p.setRound(round)
		// clean the round
		p.state.cleanRound(round)
		// send the round change message
		p.sendRoundChange()
	}
	sendNextRoundChange := func() {
		sendRoundChange(p.state.GetCurrentRound() + 1)
	}

	checkTimeout := func() {
		// At this point we might be stuck in the network if:
		// - We have advanced the round but everyone else passed.
		// - We are removing those messages since they are old now.
		if bestHeight, stucked := p.backend.IsStuck(p.state.view.Sequence); stucked {
			span.AddEvent("OutOfSync", trace.WithAttributes(
				// our local height
				attribute.Int64("local", int64(p.state.view.Sequence)),
				// the best remote height
				attribute.Int64("remote", int64(bestHeight)),
			))
			p.setState(SyncState)
			return
		}

		// otherwise, it seems that we are in sync
		// and we should start a new round
		sendNextRoundChange()
	}

	// if the round was triggered due to an error, we send our own
	// next round change
	if err := p.state.getErr(); err != nil {
		p.logger.Printf("[DEBUG] round change handle error. Error message: %v", err)
		sendNextRoundChange()
	} else {
		// otherwise, it is due to a timeout in any stage
		// First, we try to sync up with any max round already available
		if maxRound, ok := p.state.maxRound(); ok {
			p.logger.Printf("[DEBUG] round change, max round=%d", maxRound)
			sendRoundChange(maxRound)
		} else {
			// otherwise, do your best to sync up
			checkTimeout()
		}
	}

	// create a timer for the round change
	for p.getState() == RoundChangeState {
		_, span := p.tracer.Start(ctx, "RoundChangeState")

		msg, ok := p.getNextMessage(span)
		if !ok {
			// closing
			span.End()
			return
		}
		if msg == nil {
			p.logger.Print("[DEBUG] round change timeout")

			// checkTimeout will either produce a sync event and exit
			// or restart the timeout
			checkTimeout()
			span.End()
			continue
		}

		// we only expect RoundChange messages right now
		num := p.state.AddRoundMessage(msg)

		if p.config.VotingPower == nil {
			if num == p.state.NumValid() {
				// start a new round inmediatly
				p.state.SetCurrentRound(msg.View.Round)
				p.setState(AcceptState)
			} else if num == p.state.MaxFaultyNodes()+1 {
				// weak certificate, try to catch up if our round number is smaller
				if p.state.GetCurrentRound() < msg.View.Round {
					// update timer
					sendRoundChange(msg.View.Round)
				}
			}
		} else {
			totalVotingPower := totalVotingPower(p.config.VotingPower)
			validVotingPower := QuorumSizeVP(totalVotingPower)
			roundVotingPower := p.state.calculateMessagesVotingPower(p.state.roundMessages[msg.View.Round], p.config.VotingPower)
			if roundVotingPower >= validVotingPower {
				// start a new round inmediatly
				p.state.SetCurrentRound(msg.View.Round)
				p.setState(AcceptState)
			} else if roundVotingPower > MaxFaultyVP(totalVotingPower)+1 {
				// weak certificate, try to catch up if our round number is smaller
				if p.state.GetCurrentRound() < msg.View.Round {
					// update timer
					sendRoundChange(msg.View.Round)
				}
			}
		}

		p.setStateSpanAttributes(span)
		span.End()
	}
}

// --- communication wrappers ---

func (p *Pbft) sendRoundChange() {
	p.gossip(MessageReq_RoundChange)
}

func (p *Pbft) sendPreprepareMsg() {
	p.gossip(MessageReq_Preprepare)
}

func (p *Pbft) sendPrepareMsg() {
	p.gossip(MessageReq_Prepare)
}

func (p *Pbft) sendCommitMsg() {
	p.gossip(MessageReq_Commit)
}

func (p *Pbft) gossip(msgType MsgType) {
	msg := &MessageReq{
		Type: msgType,
		From: p.validator.NodeID(),
	}
	if msgType != MessageReq_RoundChange {
		// Except for round change message in which we are deciding on the proposer,
		// the rest of the consensus message require the hash:
		// 1. Preprepare: notify the validators of the proposal + hash
		// 2. Prepare + Commit: safe check to only include messages from our round.
		msg.Hash = p.state.proposal.Hash
	}

	// add View
	msg.View = p.state.view.Copy()

	// if we are sending a preprepare message we need to include the proposal
	if msg.Type == MessageReq_Preprepare {
		msg.SetProposal(p.state.proposal.Data)
	}

	// if the message is commit, we need to add the committed seal
	if msg.Type == MessageReq_Commit {
		// seal the hash of the proposal
		seal, err := p.validator.Sign(p.state.proposal.Hash)
		if err != nil {
			p.logger.Printf("[ERROR] failed to commit seal. Error message: %v", err)
			return
		}
		msg.Seal = seal
	}

	if msg.Type != MessageReq_Preprepare {
		// send a copy to ourselves so that we can process this message as well
		msg2 := msg.Copy()
		msg2.From = p.validator.NodeID()
		p.PushMessage(msg2)
	}
	if err := p.transport.Gossip(msg); err != nil {
		p.logger.Printf("[ERROR] failed to gossip. Error message: %v", err)
	}
}

// GetValidatorId returns validator NodeID
func (p *Pbft) GetValidatorId() NodeID {
	return p.validator.NodeID()
}

// GetState returns the current PBFT state
func (p *Pbft) GetState() PbftState {
	return p.getState()
}

// getState returns the current PBFT state
func (p *Pbft) getState() PbftState {
	return p.state.getState()
}

// isState checks if the node is in the passed in state
func (p *Pbft) IsState(s PbftState) bool {
	return p.state.getState() == s
}

func (p *Pbft) SetState(s PbftState) {
	p.setState(s)
}

// setState sets the PBFT state
func (p *Pbft) setState(s PbftState) {
	p.logger.Printf("[DEBUG] state change: '%s'", s)
	p.state.setState(s)
}

// IsLocked returns if the current proposal is locked
func (p *Pbft) IsLocked() bool {
	return atomic.LoadUint64(&p.state.locked) == 1
}

// GetProposal returns current proposal in the pbft
func (p *Pbft) GetProposal() *Proposal {
	return p.state.proposal
}

func (p *Pbft) Round() uint64 {
	return atomic.LoadUint64(&p.state.view.Round)
}

// getNextMessage reads a new message from the message queue
func (p *Pbft) getNextMessage(span trace.Span) (*MessageReq, bool) {
	for {
		msg, discards := p.notifier.ReadNextMessage(p)
		// send the discard messages
		p.logger.Printf("[TRACE] Current state %s, number of prepared messages: %d, number of committed messages %d", p.getState(), p.state.numPrepared(), p.state.numCommitted())

		for _, msg := range discards {
			p.logger.Printf("[TRACE] Discarded %s ", msg)
			p.spanAddEventMessage("dropMessage", span, msg)
		}
		if msg != nil {
			// add the event to the span
			p.spanAddEventMessage("message", span, msg)
			p.logger.Printf("[TRACE] Received %s", msg)
			return msg, true
		}

		// wait until there is a new message or
		// someone closes the stopCh (i.e. timeout for round change)
		select {
		case <-p.state.timeoutChan:
			span.AddEvent("Timeout")
			p.notifier.HandleTimeout(p.validator.NodeID(), stateToMsg(p.getState()), &View{
				Round:    p.state.GetCurrentRound(),
				Sequence: p.state.view.Sequence,
			})
			p.logger.Printf("[TRACE] Message read timeout occurred")
			return nil, true
		case <-p.ctx.Done():
			return nil, false
		case <-p.updateCh:
		}
	}
}

func (p *Pbft) PushMessageInternal(msg *MessageReq) {
	p.msgQueue.pushMessage(msg)

	select {
	case p.updateCh <- struct{}{}:
	default:
	}
}

// PushMessage pushes a new message to the message queue
func (p *Pbft) PushMessage(msg *MessageReq) {
	if err := msg.Validate(); err != nil {
		p.logger.Printf("[ERROR]: failed to validate msg: %v", err)
		return
	}

	p.PushMessageInternal(msg)
}

// Reads next message with discards from message queue based on current state, sequence and round
func (p *Pbft) ReadMessageWithDiscards() (*MessageReq, []*MessageReq) {
	return p.msgQueue.readMessageWithDiscards(p.getState(), p.state.view)
}

// --- package-level helper functions ---
// exponentialTimeout calculates the timeout duration depending on the current round.
// Round acts as an exponent when determining timeout (2^round).
func exponentialTimeoutDuration(round uint64) time.Duration {
	timeout := defaultTimeout
	// limit exponent to be in range of maxTimeout (<=8) otherwise use maxTimeout
	// this prevents calculating timeout that is greater than maxTimeout and
	// possible overflow for calculating timeout for rounds >33 since duration is in nanoseconds stored in int64
	if round <= maxTimeoutExponent {
		timeout += time.Duration(1<<round) * time.Second
	} else {
		timeout = maxTimeout
	}
	return timeout
}
func exponentialTimeout(round uint64) <-chan time.Time {
	return time.NewTimer(exponentialTimeoutDuration(round)).C
}

// MaxFaultyNodes calculate max faulty nodes in order to have Byzantine-fault tollerant system.
// Formula explanation:
// N -> number of nodes in PBFT
// F -> number of faulty nodes
// N = 3 * F + 1 => F = (N - 1) / 3
//
// PBFT tolerates 1 failure with 4 nodes
// 4 = 3 * 1 + 1
// To tolerate 2 failures, PBFT requires 7 nodes
// 7 = 3 * 2 + 1
// It should always take the floor of the result
func MaxFaultyNodes(nodesCount int) int {
	if nodesCount <= 0 {
		return 0
	}
	return (nodesCount - 1) / 3
}

// QuorumSize calculates quorum size (namely the number of required messages of some type in order to proceed to the next state in PolyBFT state machine).
// It is calculated by formula:
// 2 * F + 1, where F denotes maximum count of faulty nodes in order to have Byzantine fault tollerant property satisfied.
func QuorumSize(nodesCount int) int {
	return 2*MaxFaultyNodes(nodesCount) + 1
}

func totalVotingPower(mp map[NodeID]uint64) uint64 {
	var totalVotingPower uint64
	for _, v := range mp {
		totalVotingPower += v
	}
	return totalVotingPower
}

func MaxFaultyVP(vp uint64) uint64 {
	if vp == 0 {
		return 0
	}
	return (vp - 1) / 3
}

func QuorumSizeVP(totalVP uint64) uint64 {
	return 2*MaxFaultyVP(totalVP) + 1
}
