package transport

import (
	"math/rand"
	"time"
)

func timeJitter(jitterMax time.Duration) time.Duration {
	return time.Duration(uint64(rand.Int63()) % uint64(jitterMax)) //nolint:golint,gosec
}
