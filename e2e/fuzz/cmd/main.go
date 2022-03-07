package main

import (
	"flag"
	"log"
	"math/rand"
	"time"

	"github.com/0xPolygon/pbft-consensus/e2e/fuzz"
	"github.com/0xPolygon/pbft-consensus/e2e/fuzz/types"
)

func main() {
	initialNodesCount := flag.Uint("nodes", 5, "Count of initially started nodes")
	duration := flag.Duration("duration", 25*time.Minute, "Duration of fuzz daemon running")
	flag.Parse()
	log.Printf("Starting PolyBFT fuzz runner...")
	log.Printf("Node count: %v\n", *initialNodesCount)
	log.Printf("Duration: %v\n", *duration)
	rand.Seed(time.Now().Unix())

	stateHandler := &types.RoundMessageHandler{}
	runner := fuzz.NewRunner(*initialNodesCount, stateHandler)
	err := runner.Run(*duration)
	if err != nil {
		log.Printf("Error while running PolyBFT fuzz runner: %s\n", err)
	} else {
		log.Println("PolyBFT fuzz runner is stopped.")
	}
	stateHandler.CloseFile()
}
