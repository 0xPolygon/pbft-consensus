package e2e

import (
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func generateNodeNames(from int, count int, prefix string) []string {
	var names []string
	for j := from; j < count; j++ {
		names = append(names, prefix+strconv.Itoa(j))
	}
	return names
}

func executeInTimerAndWait(tickTime time.Duration, duration time.Duration, fn func(time.Duration)) {
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
	r := rand.Intn(101)
	return r >= threshold
}

func isFuzzEnabled(t *testing.T) {
	if os.Getenv("FUZZ") != "true" {
		t.Skip("Fuzz tests are disabled.")
	}
}

func CreateLogsDir(t *testing.T) (string, error) {
	//logs directory will be generated at the root of the e2e project
	var logsDir, logsDirName string
	var err error

	if t != nil {
		logsDirName = t.Name()
	} else {
		logsDirName = "logs"
	}

	if os.Getenv("E2E_LOG_TO_FILES") == "true" {
		logsDir, err = os.MkdirTemp("../", logsDirName+"-")
	}

	return logsDir, err
}

func GetLoggerOutput(name string, logsDir string) io.Writer {
	var loggerOutput io.Writer
	var err error
	if os.Getenv("SILENT") == "true" {
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
