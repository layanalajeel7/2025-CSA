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

	// set up world slice (height rows, width columns)
	world := make([][]uint8, p.ImageHeight)
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth)
	}

	// load initial pgm file using IO goroutine
	filename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	// fill the world from input
	// and tell SDL any cells that are alive at time 0
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			cell := <-c.ioInput
			world[y][x] = cell
			if cell == 255 {
				c.events <- CellFlipped{0, util.Cell{X: x, Y: y}}
			}
		}
	}

	// first frame is ready, so send turn 0 complete
	c.events <- TurnComplete{CompletedTurns: 0}

	// move into executing state
	c.events <- StateChange{0, Executing}

	// connect to broker (server side)
	client, err := rpc.Dial("tcp", "localhost:8080")
	if err != nil {
		fmt.Println("Could not connect to broker:", err)
		c.events <- StateChange{0, Quitting}
		close(c.events)
		return
	}

	// package the request for the server
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

	// we need this to stop the ticker when everything ends
	done := make(chan struct{})

	// every 2 seconds ask broker for alive count
	// this is needed only because TestAlive checks it
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

				// SDL will not draw unless it gets this
				c.events <- TurnComplete{turn}

			case <-done:
				return
			}
		}
	}()

	// get a fresh client for keypress stuff
	client, _ = rpc.Dial("tcp", "localhost:8080")

	// keyboard handling (pause, snapshot, quit)
	go func() {
		paused := false

		for key := range keyPresses {

			switch key {

			case 's':
				var snapRes stubs.RunGolResponse
				_ = client.Call(stubs.RunGolHandler, req, &snapRes)

				c.ioCommand <- ioOutput
				name := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, snapRes.GolBoard.CurrentTurn)
				c.ioFilename <- name

				// write snapshot world out
				for y := 0; y < p.ImageHeight; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						c.ioOutput <- snapRes.GolBoard.World[y][x]
					}
				}

				c.ioCommand <- ioCheckIdle
				<-c.ioIdle

				c.events <- ImageOutputComplete{snapRes.GolBoard.CurrentTurn, name}

			case 'q':
				// SDL expects events to stop
				close(c.events)
				return

			case 'k':
				close(c.events)
				return

			case 'p':
				if !paused {
					paused = true
					c.events <- StateChange{res.GolBoard.CurrentTurn, Paused}
				} else {
					paused = false
					c.events <- StateChange{res.GolBoard.CurrentTurn, Executing}
				}
			}
		}
	}()

	// actually tell broker to run everything
	err = client.Call(stubs.RunGolHandler, req, &res)
	if err != nil {
		fmt.Println("RPC error:", err)
		c.events <- StateChange{0, Quitting}
		close(c.events)
		return
	}

	// final world returned by server
	finalWorld := res.GolBoard.World
	finalTurn := res.GolBoard.CurrentTurn

	// write out final pgm
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

	// build list of alive cells for tests
	var alive []util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if finalWorld[y][x] == 255 {
				alive = append(alive, util.Cell{X: x, Y: y})
			}
		}
	}

	// final events expected by SDL/tests
	c.events <- FinalTurnComplete{finalTurn, alive}
	c.events <- StateChange{finalTurn, Quitting}

	close(done)
	close(c.events)
}
