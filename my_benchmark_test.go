package main

import (
	"log"
	"testing"
	"time"

	"uk.ac.bris.cs/gameoflife/gol"
)

// Runs ONE benchmark and returns time in ns.
func runBenchmark(width, height, turns, threads int) float64 {
	start := time.Now()

	events := make(chan gol.Event, 200000)
	keyPresses := make(chan rune, 10)

	// Drain events so Run() never blocks
	go func() {
		for range events {
		}
	}()

	params := gol.Params{
		ImageWidth:  width,
		ImageHeight: height,
		Turns:       turns,
		Threads:     threads,
	}

	gol.Run(params, events, keyPresses)
	return time.Since(start).Seconds()
}

// Runs the full suite of benchmarks.
func runAllBenchmarks() {
	width := 512
	height := 512

	turnsList := []int{1000, 10000}
	threadsList := []int{1, 2, 4, 8, 16}

	log.Printf("[Bench] %-10v %v", "Width", width)
	log.Printf("[Bench] %-10v %v", "Height", height)
	log.Printf("[Bench] %-10v %v", "TurnsList", turnsList)
	log.Printf("[Bench] %-10v %v", "ThreadsList", threadsList)
	log.Printf("--------------------------------------------")

	for _, turns := range turnsList {
		log.Printf("\n[Bench] ---- %d TURNS ----", turns)

		for _, threads := range threadsList {
			ns := runBenchmark(width, height, turns, threads)
			log.Printf("[Bench] Turns=%-6d Threads=%-3d Time=%f s",
				turns, threads, ns)
		}
	}

	log.Printf("\n[Bench] END OF BENCHMARKS")
}

// Wrapper for go test
func TestBenchmarks(t *testing.T) {
	runAllBenchmarks()
}
