package ibft

type Transport interface {
	Gossip(msg *MessageReq) error
}
