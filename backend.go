package pbft

type Backend interface {
	// BuildProposal builds a proposal for the current round (used if proposer)
	BuildProposal() (*Proposal, error)

	// Height returns the height for the current round
	Height() uint64

	// Init is used to signal the backend that a new round is going to start.
	Init(*RoundInfo)

	// Insert inserts the sealed proposal
	Insert(p *SealedProposal) error

	// IsStuck returns whether the pbft is stucked
	IsStuck(num uint64) (uint64, bool)

	// Validate validates a raw proposal (used if non-proposer)
	Validate(*Proposal) error

	// ValidatorSet returns the validator set for the current round
	ValidatorSet() ValidatorSet

	// ValidateCommit is used to validate that a given commit is valid
	ValidateCommit(from NodeID, seal []byte) error
}
