package fuzzy

import (
	"fmt"
	"testing"
	"time"
)

func TestIBFT_NoIssue(t *testing.T) {
	c := newIBFTCluster(t, "noissue", 5)
	c.Start()

	time.Sleep(5 * time.Second)
	c.Stop()

	fmt.Println(c)
}
