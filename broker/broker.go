package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/rpc"

	"uk.ac.bris.cs/gameoflife/stubs"
)

type Broker struct {
	currentBoard stubs.GolBoard
}

// countNeighbours checks 8 neighbours with toroidal wrapping.
func countNeighbours(world [][]uint8, x, y, w, h int) int {
	alive := 0

	for j := -1; j <= 1; j++ {
		for i := -1; i <= 1; i++ {
			if i == 0 && j == 0 {
				continue
			}
			ny := (y + j + h) % h
			nx := (x + i + w) % w

			if world[ny][nx] == 255 {
				alive++
			}
		}
	}

	return alive
}

// applyRules is the "secret" GOL logic
func applyRules(world [][]uint8, turns int) [][]uint8 {
	h := len(world)
	w := len(world[0])

	curr := world

	for t := 0; t < turns; t++ {
		next := make([][]uint8, h)
		for y := 0; y < h; y++ {
			next[y] = make([]uint8, w)
		}

		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {

				neighbours := countNeighbours(curr, x, y, w, h)
				cell := curr[y][x]

				if cell == 255 {
					if neighbours == 2 || neighbours == 3 {
						next[y][x] = 255
					} else {
						next[y][x] = 0
					}
				} else {
					if neighbours == 3 {
						next[y][x] = 255
					} else {
						next[y][x] = 0
					}
				}
			}
		}

		curr = next
	}

	return curr
}

//broker type

// RunGol is the RPC method
func (b *Broker) RunGol(req stubs.RunGolRequest, res *stubs.RunGolResponse) error {

	if req.GolBoard.World == nil {
		return errors.New("empty world received by broker")
	}

	// important: store initial board for early alive counts
	b.currentBoard = req.GolBoard

	// run all turns
	finalWorld := applyRules(req.GolBoard.World, req.Turns)

	// update broker board
	b.currentBoard = stubs.GolBoard{
		World:       finalWorld,
		Width:       req.GolBoard.Width,
		Height:      req.GolBoard.Height,
		CurrentTurn: req.Turns,
	}

	// return final world to client
	res.GolBoard = b.currentBoard

	return nil
}

func (b *Broker) GetAliveCount(req stubs.AliveCountRequest, res *stubs.AliveCountResponse) error {
	count := 0
	for y := 0; y < b.currentBoard.Height; y++ {
		for x := 0; x < b.currentBoard.Width; x++ {
			if b.currentBoard.World[y][x] == 255 {
				count++
			}
		}
	}

	res.Count = count
	return nil
}

// main server loop
func main() {

	port := flag.String("port", "8080", "Port to listen on")
	flag.Parse()

	err := rpc.Register(new(Broker))
	if err != nil {
		fmt.Println("Failed to register broker:", err)
		return
	}

	listener, err := net.Listen("tcp", ":"+*port)
	if err != nil {
		fmt.Println("Listen error:", err)
		return
	}

	fmt.Println("Broker running on port", *port)
	defer listener.Close()

	rpc.Accept(listener)
}
