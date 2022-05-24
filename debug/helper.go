package debug

import (
	"fmt"
	"runtime"
	"strings"
)

func Line() string {
	return LineN(1)
}

func LineN(n int) string {
	_, file, line, _ := runtime.Caller(n + 1)
	file = strings.TrimPrefix(file, "/Users/boris/go/src/github.com/0xPolygon/pbft-consensus/")
	file = strings.TrimPrefix(file, "e2e/")
	return fmt.Sprintf("%s:%d", file, line)

}
