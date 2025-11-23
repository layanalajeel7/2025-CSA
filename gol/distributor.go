package gol

import (
	"fmt"
	"net/rpc"
	"time"

	"uk.ac.bris.cs/gameoflife/stubs"
	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

func distributor(p Params, c distributorChannels, keyPresses <-chan rune) {

	world := make([][]uint8, p.ImageHeight)
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth)
	}

	filename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			cell := <-c.ioInput
			world[y][x] = cell
			if cell == 255 {
				c.events <- CellFlipped{0, util.Cell{X: x, Y: y}}
			}
		}
	}

	// ⭐ VERY IMPORTANT: draw initial frame so SDL is not black
	c.events <- TurnComplete{CompletedTurns: 0}

	c.events <- StateChange{0, Executing}

	client, err := rpc.Dial("tcp", "localhost:8080")
	if err != nil {
		fmt.Println("Could not connect to broker:", err)
		c.events <- StateChange{0, Quitting}
		close(c.events)
		return
	}

	req := stubs.RunGolRequest{
		GolBoard: stubs.GolBoard{
			World:  world,
			Width:  p.ImageWidth,
			Height: p.ImageHeight,
		},
		Turns:   p.Turns,
		Threads: p.Threads,
	}

	var res stubs.RunGolResponse

	done := make(chan struct{})

	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		turn := 0

		for {
			select {
			case <-t.C:
				var alive stubs.AliveCountResponse
				err := client.Call(stubs.GetAliveCountHandler, stubs.AliveCountRequest{}, &alive)
				if err != nil {
					return
				}

				turn++
				c.events <- AliveCellsCount{turn, alive.Count}

				// Give SDL a frame every 2s so screen updates
				c.events <- TurnComplete{turn}

			case <-done:
				return
			}
		}
	}()

	go func() {
		paused := false
		for key := range keyPresses {

			switch key {

			case 's':
				// snapshot current board from broker
				_ = client.Call(stubs.RunGolHandler, req, &res)

				c.ioCommand <- ioOutput
				name := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, res.GolBoard.CurrentTurn)
				c.ioFilename <- name

				for y := 0; y < p.ImageHeight; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						c.ioOutput <- res.GolBoard.World[y][x]
					}
				}
				c.ioCommand <- ioCheckIdle
				<-c.ioIdle

				c.events <- ImageOutputComplete{res.GolBoard.CurrentTurn, name}

			case 'q':
				fmt.Println("Controller quitting…")
				c.events <- StateChange{res.GolBoard.CurrentTurn, Quitting}
				close(c.events)
				return

			case 'k':
				fmt.Println("K pressed — shutting down locally")
				c.events <- StateChange{res.GolBoard.CurrentTurn, Quitting}
				close(c.events)
				return

			case 'p':
				if !paused {
					paused = true
					fmt.Println("Paused")
					c.events <- StateChange{res.GolBoard.CurrentTurn, Paused}
				} else {
					paused = false
					fmt.Println("Continuing")
					c.events <- StateChange{res.GolBoard.CurrentTurn, Executing}
				}
			}
		}
	}()

	err = client.Call(stubs.RunGolHandler, req, &res)
	if err != nil {
		fmt.Println("RPC error:", err)
		c.events <- StateChange{0, Quitting}
		close(c.events)
		return
	}

	finalWorld := res.GolBoard.World
	finalTurn := res.GolBoard.CurrentTurn

	outName := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, finalTurn)

	c.ioCommand <- ioOutput
	c.ioFilename <- outName

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- finalWorld[y][x]
		}
	}

	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- ImageOutputComplete{finalTurn, outName}

	var alive []util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if finalWorld[y][x] == 255 {
				alive = append(alive, util.Cell{X: x, Y: y})
			}
		}
	}

	c.events <- FinalTurnComplete{finalTurn, alive}
	c.events <- StateChange{finalTurn, Quitting}

	close(done)
	close(c.events)
}
