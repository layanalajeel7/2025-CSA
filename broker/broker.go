package main

import (
	"net"
	"net/rpc"
	"uk.ac.bris.cs/gameoflife/stubs"
)

// Remote worker type
type Engine struct{}

// evolve the world locally on AWS
func step(world [][]uint8, w, h int) [][]uint8 {
	next := make([][]uint8, h)
	for y := range next {
		next[y] = make([]uint8, w)
	}

	count := func(x, y int) int {
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

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := world[y][x]
			a := count(x, y)

			if c == 255 {
				if a < 2 || a > 3 {
					next[y][x] = 0
				} else {
					next[y][x] = 255
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
	return next
}

// RPC endpoint for Stage 2
func (e *Engine) RunGol(req stubs.RunGolRequest, res *stubs.RunGolResponse) error {

	w := req.GolBoard.Width
	h := req.GolBoard.Height
	world := req.GolBoard.World

	for i := 0; i < req.Turns; i++ {
		world = step(world, w, h)
	}

	res.GolBoard.World = world
	res.GolBoard.Width = w
	res.GolBoard.Height = h
	res.GolBoard.CurrentTurn = req.Turns
	return nil
}

func main() {
	rpc.Register(&Engine{})
	l, _ := net.Listen("tcp", ":8080")
	rpc.Accept(l)
}
