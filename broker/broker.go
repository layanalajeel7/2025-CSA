package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/rpc"

	"uk.ac.bris.cs/gameoflife/stubs"
)

// just storing the initial board + how many times alive count was asked
type Broker struct {
	initial stubs.GolBoard // turn 0 basically
	polls   int            // how many GetAliveCount() calls happened
}

// literally one GOL step helper
func stepOnce(in [][]uint8, h, w int) [][]uint8 {
	next := make([][]uint8, h) // new board
	for y := 0; y < h; y++ {
		next[y] = make([]uint8, w)
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {

			alive := 0 // count alive
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if dx == 0 && dy == 0 {
						continue // skip itself
					}
					ny := (y + dy + h) % h // wrap around
					nx := (x + dx + w) % w
					if in[ny][nx] == 255 { // alive cell
						alive++
					}
				}
			}

			cell := in[y][x]
			newVal := cell // default is “stay the same”

			// conway rules again (exact same as spec)
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

	return next // return new board after 1 turn
}

// distributor calls this ONCE to run all turns fully, then get final board
func (b *Broker) RunGol(req stubs.RunGolRequest, res *stubs.RunGolResponse) error {
	if req.GolBoard.World == nil {
		return errors.New("empty world received by broker") // just in case
	}

	h := req.GolBoard.Height
	w := req.GolBoard.Width
	turns := req.Turns

	// copy initial board so we don't accidentally modify what distributor sent
	initWorld := make([][]uint8, h)
	for y := 0; y < h; y++ {
		initWorld[y] = make([]uint8, w)
		copy(initWorld[y], req.GolBoard.World[y]) // manual deep copy
	}

	// store this so GetAliveCount() can rebuild turn 1,2,3 etc
	b.initial = stubs.GolBoard{
		World:       initWorld,
		Width:       w,
		Height:      h,
		CurrentTurn: 0,
	}
	b.polls = 0 // reset alive-count counter every new run

	// now actually simulate ALL turns
	curr := initWorld
	for t := 0; t < turns; t++ {
		curr = stepOnce(curr, h, w)
	}

	// send back final board + final turn number
	res.GolBoard = stubs.GolBoard{
		World:       curr,
		Width:       w,
		Height:      h,
		CurrentTurn: turns,
	}

	return nil
}

// This is called repeatedly by distributor’s ticker every 2 seconds.
// First time = board after 1 turn,
// second time = after 2 turns,
// etc.
func (b *Broker) GetAliveCount(req stubs.AliveCountRequest, res *stubs.AliveCountResponse) error {

	if b.initial.World == nil {
		// RunGol hasn't been called yet
		res.Count = 0
		return nil
	}

	h := b.initial.Height
	w := b.initial.Width

	// start from scratch (turn 0 board)
	curr := make([][]uint8, h)
	for y := 0; y < h; y++ {
		curr[y] = make([]uint8, w)
		copy(curr[y], b.initial.World[y]) // deep copy again
	}

	b.polls++        // 1st call → 1, 2nd → 2, etc
	steps := b.polls // how many steps we should simulate
	for i := 0; i < steps; i++ {
		curr = stepOnce(curr, h, w) // re-simulate up to that turn
	}

	// count alive cells in that reconstructed world
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

	_ = rpc.Register(new(Broker)) // register broker as RPC server

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		fmt.Println("Listen error:", err)
		return
	}

	fmt.Println("Broker running on port", *port)
	defer listener.Close()

	rpc.Accept(listener) // just sits and waits for requests
}
