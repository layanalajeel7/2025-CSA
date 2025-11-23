package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/rpc"

	"uk.ac.bris.cs/gameoflife/stubs"
)

// countNeighbours checks 8 neighbours with toroidal wrapping.
func countNeighbours(world [][]uint8, x, y, w, h int) int {
	n := 0
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			ny := (y + dy + h) % h
			nx := (x + dx + w) % w
			if world[ny][nx] == 255 {
				n++
			}
		}
	}
	return n
}

// applyRules is the "secret" GOL logic
func applyRules(world [][]uint8, turns int) [][]uint8 {
	h := len(world)
	if h == 0 {
		return world
	}
	w := len(world[0])

	curr := world

	for t := 0; t < turns; t++ {

		next := make([][]uint8, h)
		for y := range next {
			next[y] = make([]uint8, w)
			for x := 0; x < w; x++ {
				a := countNeighbours(curr, x, y, w, h)
				c := curr[y][x]

				if c == 255 {
					if a == 2 || a == 3 {
						next[y][x] = 255
					} else {
						next[y][x] = 0
					}
				} else {
					if a == 3 {
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

type Broker struct{}

// RunGol is the RPC method
func (b *Broker) RunGol(req stubs.RunGolRequest, res *stubs.RunGolResponse) error {

	if req.GolBoard.World == nil {
		return errors.New("empty world received by broker")
	}

	finalWorld := applyRules(req.GolBoard.World, req.Turns)

	res.GolBoard = stubs.GolBoard{
		World:       finalWorld,
		Width:       req.GolBoard.Width,
		Height:      req.GolBoard.Height,
		CurrentTurn: req.Turns,
	}

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
