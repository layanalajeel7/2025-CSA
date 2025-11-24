package tests

import (
	"testing"
	"uk.ac.bris.cs/gameoflife/gol"
)

func runDistributedBenchmark(b *testing.B, width, height, turns, threads int) {
	params := gol.Params{
		ImageWidth:  width,
		ImageHeight: height,
		Turns:       turns,
		Threads:     threads,
	}

	for n := 0; n < b.N; n++ {
		func() {
			// Protect the benchmark from distributor panics
			defer func() { recover() }()

			events := make(chan gol.Event)
			keyPresses := make(chan rune)

			// Start the simulation
			go gol.Run(params, events, keyPresses)

			// Drain all events until distributor closes the channel
			for range events {
			}
		}()
	}
}

func BenchmarkDistributed100x100(b *testing.B) {
	runDistributedBenchmark(b, 100, 100, 100, 8)
}

func BenchmarkDistributed256x256(b *testing.B) {
	runDistributedBenchmark(b, 256, 256, 200, 8)
}

func BenchmarkDistributed512x512(b *testing.B) {
	runDistributedBenchmark(b, 512, 512, 500, 8)
}
