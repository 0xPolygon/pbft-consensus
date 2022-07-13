package pbft

import (
	"log"
	"os"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/0xPolygon/pbft-consensus/stats"
)

const (
	defaultTimeout     = 2 * time.Second
	maxTimeout         = 300 * time.Second
	maxTimeoutExponent = 8
)

type RoundTimeout func(round uint64) <-chan time.Time

type StatsCallback func(stats.Stats)

type ConfigOption func(*Config)

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

// RoundInfo is the information about the round
type RoundInfo struct {
	IsProposer bool
	Proposer   NodeID
	Locked     bool
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
