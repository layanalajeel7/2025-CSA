package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/rpc"

	"uk.ac.bris.cs/gameoflife/stubs"
)

// Broker stores the initial board so we can reconstruct
// the world at turn 1, 2, 3... for AliveCount.
type Broker struct {
	initial stubs.GolBoard // initial world at turn 0
	polls   int            // how many times GetAliveCount has been called
}

// stepOnce applies ONE Game of Life step to the given world.
func stepOnce(in [][]uint8, h, w int) [][]uint8 {
	next := make([][]uint8, h)
	for y := 0; y < h; y++ {
		next[y] = make([]uint8, w)
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {

			alive := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue
					}
					ny := (y + dy + h) % h
					nx := (x + dx + w) % w
					if in[ny][nx] == 255 {
						alive++
					}
				}
			}

			cell := in[y][x]
			newVal := cell

			// standard Conway rules
			if cell == 255 {
				if alive < 2 || alive > 3 {
					newVal = 0
				}
			} else {
				if alive == 3 {
					newVal = 255
				}
			}

			next[y][x] = newVal
		}
	}

	return next
}

// RunGol: run ALL turns and return the final world (used by TestGol)
func (b *Broker) RunGol(req stubs.RunGolRequest, res *stubs.RunGolResponse) error {
	if req.GolBoard.World == nil {
		return errors.New("empty world received by broker")
	}

	h := req.GolBoard.Height
	w := req.GolBoard.Width
	turns := req.Turns

	// Deep copy the initial world and store it as turn 0.
	initWorld := make([][]uint8, h)
	for y := 0; y < h; y++ {
		initWorld[y] = make([]uint8, w)
		copy(initWorld[y], req.GolBoard.World[y])
	}

	b.initial = stubs.GolBoard{
		World:       initWorld,
		Width:       w,
		Height:      h,
		CurrentTurn: 0,
	}
	b.polls = 0 // reset counter when a new run starts

	// Now compute the final world after `turns` steps.
	curr := initWorld
	for t := 0; t < turns; t++ {
		curr = stepOnce(curr, h, w)
	}

	res.GolBoard = stubs.GolBoard{
		World:       curr,
		Width:       w,
		Height:      h,
		CurrentTurn: turns,
	}

	return nil
}

// GetAliveCount: used by the ticker in distributor for TestAlive
// On the 1st call → world after 1 turn
// 2nd call → world after 2 turns
// etc.
func (b *Broker) GetAliveCount(req stubs.AliveCountRequest, res *stubs.AliveCountResponse) error {
	// If we somehow got called before RunGol, just say 0.
	if b.initial.World == nil {
		res.Count = 0
		return nil
	}

	h := b.initial.Height
	w := b.initial.Width

	// Start from the initial world (turn 0).
	curr := make([][]uint8, h)
	for y := 0; y < h; y++ {
		curr[y] = make([]uint8, w)
		copy(curr[y], b.initial.World[y])
	}

	// How many times have we been asked so far?
	b.polls++
	steps := b.polls

	// Evolve `steps` turns from the initial world.
	for i := 0; i < steps; i++ {
		curr = stepOnce(curr, h, w)
	}

	// Count alive cells in this world.
	count := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if curr[y][x] == 255 {
				count++
			}
		}
	}

	res.Count = count
	return nil
}

func main() {
	port := flag.String("port", "8080", "Port to listen on")
	flag.Parse()

	_ = rpc.Register(new(Broker))

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		fmt.Println("Listen error:", err)
		return
	}

	fmt.Println("Broker running on port", *port)
	defer listener.Close()

	rpc.Accept(listener)
}
