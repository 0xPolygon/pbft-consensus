package main

import (
	"context"
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e/fuzz"
)

func main() {
	// Define flags
	initialNodesCount := flag.Uint("nodes", 5, "Count of initially started nodes")
	duration := flag.Duration("duration", 1*time.Minute, "Duration of fuzz daemon running")
	flag.Parse()
	log.Printf("Starting PolyBFT fuzz runner...")
	log.Printf("Node count: %v\n", *initialNodesCount)
	log.Printf("Duration: %v\n", *duration)

	rand.Seed(time.Now().Unix())

	ctx, cancelFn := context.WithTimeout(context.Background(), *duration)
	defer cancelFn()

	// Setup a runner and run it
	runner := fuzz.Setup(*initialNodesCount)
	runner.Run(ctx)

	log.Println("PolyBFT fuzz runner is stopped.")
}
