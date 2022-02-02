package main

import (
	"context"
	"log"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"time"
)

type Runner interface {
	Apply()
}

type DropNode struct {
	// cluster ref
	nodeCnt          int
	nodesToDrop      int
	droppingInterval time.Duration
	duration         time.Duration
}

func (dn *DropNode) Apply() {
	ticker := time.NewTicker(dn.droppingInterval)
	after := time.After(dn.duration)
	for {
		select {
		case <-ticker.C:
			log.Println("Dropping node.")
		case <-after:
			ticker.Stop()
			log.Println("Done dropping nodes.")
			return
		}
	}
}

// example of scenario
var dn = DropNode{
	nodeCnt:          5,
	nodesToDrop:      2,
	droppingInterval: 1 * time.Second,
	duration:         50 * time.Second,
}
var scenarios = []Runner{&dn}

func main() {
	args := os.Args[1:]
	nodeCnt, err := strconv.Atoi(args[0])
	if err != nil {
		log.Printf("Invalid node arguments:%s\n", err)
		os.Exit(1)
	}
	log.Printf("Node count %v\n", nodeCnt)

	rand.Seed(time.Now().Unix())
	wg := sync.WaitGroup{}
	wg.Add(1)

	// define how long the runner will be executing the scenario
	ctx, cancelFn := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancelFn()
	// pass scenario
	go func(ctx context.Context) {
		// select test case and validate the end state against the cluster
		pick := 0
		select {
		case <-ctx.Done():
			log.Println("Done executing ")
		default:
			scenarios[pick].Apply()
		}
		wg.Done()
	}(ctx)

	// validate the progress
	wg.Wait()

	log.Println("Exiting...")
}
