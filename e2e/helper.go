package e2e

import "strconv"

func generateNodeNames(from int, count int, prefix string) []string {
	var names []string
	for j := from; j < count; j++ {
		names = append(names, prefix+strconv.Itoa(j))
	}
	return names
}
