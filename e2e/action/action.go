package action

import (
	"github.com/0xPolygon/pbft-consensus/e2e"
)

type RevertFunc func()

type Action interface {
	CanApply(c *e2e.Cluster) bool
	Apply(c *e2e.Cluster) RevertFunc
}

Action, Hook