package helper

import (
	//nolint:golint,gosec
	"crypto/sha1"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/0xPolygon/pbft-consensus"
)

const trueString = "true"

func Hash(p []byte) []byte {
	h := sha1.New() //nolint:golint,gosec
	h.Write(p)
	return h.Sum(nil)
}

func GenerateProposal() []byte {
	prop := make([]byte, 4)
	_, _ = rand.Read(prop) //nolint:golint,gosec
	return prop
}

func GenerateNodeNames(from int, count int, prefix string) []string {
	var names []string
	for j := from; j < count; j++ {
		names = append(names, prefix+strconv.Itoa(j))
	}
	return names
}

func ExecuteInTimerAndWait(tickTime time.Duration, duration time.Duration, fn func(time.Duration)) {
	end := executeInTimer(tickTime, duration, fn)
	<-end
}

func executeInTimer(tickTime time.Duration, duration time.Duration, fn func(time.Duration)) chan struct{} {
	tick := time.NewTicker(tickTime)
	tickerDone := make(chan struct{})
	end := make(chan struct{})
	startTime := time.Now()
	go func() {
		for {
			select {
			case v := <-tick.C:
				elapsedTime := v.Sub(startTime)
				fn(elapsedTime)
			case <-tickerDone:
				close(end)
				return
			}
		}
	}()

	after := time.After(duration)
	go func() {
		<-after
		tick.Stop()
		close(tickerDone)
	}()
	return end
}

func Contains(nodes []string, node string) bool {
	for _, n := range nodes {
		if n == node {
			return true
		}
	}

	return false
}

// ShouldApply is used to check if random event meets the threshold
func ShouldApply(threshold int) bool {
	r := rand.Intn(101) //nolint:golint,gosec
	return r >= threshold
}

func IsFuzzEnabled(t *testing.T) {
	if os.Getenv("FUZZ") != trueString {
		t.Skip("Fuzz tests are disabled.")
	}
}

func CreateLogsDir(directoryName string) (string, error) {
	//logs directory will be generated at the root of the e2e project
	var logsDir string
	var err error

	if directoryName == "" {
		directoryName = fmt.Sprintf("logs_%v", time.Now().Format(time.RFC3339))
	}

	if os.Getenv("E2E_LOG_TO_FILES") == trueString {
		logsDir, err = os.MkdirTemp("../", directoryName+"-")
	}

	return logsDir, err
}

func GetLoggerOutput(name string, logsDir string) io.Writer {
	var loggerOutput io.Writer
	var err error
	if os.Getenv("SILENT") == trueString {
		loggerOutput = ioutil.Discard
	} else if logsDir != "" {
		loggerOutput, err = os.OpenFile(filepath.Join(logsDir, name+".log"), os.O_RDWR|os.O_APPEND|os.O_CREATE, 0660)
		if err != nil {
			log.Printf("[WARNING] Failed to open file for node: %v. Reason: %v. Fallbacked to standard output.", name, err)
			loggerOutput = os.Stdout
		}
	} else {
		loggerOutput = os.Stdout
	}
	return loggerOutput
}

// GetPredefinedTimeout is a closure to the function which is returning given predefined timeout.
func GetPredefinedTimeout(timeout time.Duration) pbft.RoundTimeout {
	return func(u uint64) <-chan time.Time {
		return time.NewTimer(timeout).C
	}
}

// IsTimeoutMessage checks if message in .flow file represents a timeout
func IsTimeoutMessage(message *pbft.MessageReq) bool {
	return message.Hash == nil && message.Proposal == nil && message.Seal == nil && message.From == ""
}
