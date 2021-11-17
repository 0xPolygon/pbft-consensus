package pbft

// Transport is a generic interface for a gossip transport protocol
type Transport interface {
	// Gossip broadcast the message to the network
	Gossip(msg *MessageReq) error
}
