package e2e

import "strconv"

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
