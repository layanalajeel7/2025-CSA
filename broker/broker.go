package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/rpc"
	"sync"

	"uk.ac.bris.cs/gameoflife/stubs"
)

// Broker keeps the starting board and how many times we've been asked
// for an alive count. mu protects these shared fields.
type Broker struct {
	mu      sync.Mutex
	initial stubs.GolBoard
	polls   int
}

// stepOnce runs one turn of Game of Life on a 2D world.
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

// RunGol runs all the turns once and returns the final board.
func (b *Broker) RunGol(req stubs.RunGolRequest, res *stubs.RunGolResponse) error {
	if req.GolBoard.World == nil {
		return errors.New("empty world received by broker")
	}

	h := req.GolBoard.Height
	w := req.GolBoard.Width
	turns := req.Turns

	// copy the starting board so we don't mutate the caller's slice
	initWorld := make([][]uint8, h)
	for y := 0; y < h; y++ {
		initWorld[y] = make([]uint8, w)
		copy(initWorld[y], req.GolBoard.World[y])
	}

	// save this board so GetAliveCount can rebuild later turns
	b.mu.Lock()
	b.initial = stubs.GolBoard{
		World:       initWorld,
		Width:       w,
		Height:      h,
		CurrentTurn: 0,
	}
	b.polls = 0
	b.mu.Unlock()

	// run all the turns on the broker
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

// GetAliveCount reconstructs the board after N turns and counts live cells.
// Each time it's called, we move one step further in time.
func (b *Broker) GetAliveCount(req stubs.AliveCountRequest, res *stubs.AliveCountResponse) error {
	b.mu.Lock()

	if b.initial.World == nil {
		b.mu.Unlock()
		res.Count = 0
		return nil
	}

	h := b.initial.Height
	w := b.initial.Width
	initWorld := b.initial.World

	b.polls++
	steps := b.polls

	b.mu.Unlock()

	curr := make([][]uint8, h)
	for y := 0; y < h; y++ {
		curr[y] = make([]uint8, w)
		copy(curr[y], initWorld[y])
	}

	for i := 0; i < steps; i++ {
		curr = stepOnce(curr, h, w)
	}

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
