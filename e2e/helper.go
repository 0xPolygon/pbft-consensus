package e2e

import (
	"strconv"
	"time"
)

func generateNodeNames(from int, count int, prefix string) []string {
	names := make([]string, count)
	for j := 0; j < count; j++ {
		names[j] = prefix + strconv.Itoa(from)
		from++
	}
	return names
}

func getPartitions(nodesCnt int, nodesPerPartition int, prefix string) [][]string {
	numberOfPartitions := (nodesCnt + nodesPerPartition - 1) / nodesPerPartition
	partitions := make([][]string, numberOfPartitions)

	for i := 0; i < numberOfPartitions; i++ {
		nodesInPartition := nodesPerPartition
		startIndex := i * nodesPerPartition
		if startIndex+nodesPerPartition > nodesCnt {
			nodesInPartition = nodesCnt - startIndex
		}

		partitions[i] = generateNodeNames(startIndex, nodesInPartition, prefix)
	}
	return partitions
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
				ellapsedTime := v.Sub(startTime)
				fn(ellapsedTime)
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
