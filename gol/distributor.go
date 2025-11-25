package gol

import (
	"fmt"
	"net/rpc"
	"runtime"
	"sync"
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

	// (height rows, width columns)
	world := make([][]uint8, p.ImageHeight)
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth)
	}

	// loading initial pgm file using IO goroutine
	filename := fmt.Sprintf("%dx%d", p.ImageWidth, p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	// fill the world from input & tell SDL any cells that are alive at time 0
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			cell := <-c.ioInput
			world[y][x] = cell
			if cell == 255 {
				c.events <- CellFlipped{0, util.Cell{X: x, Y: y}}
			}
		}
	}

	c.events <- TurnComplete{CompletedTurns: 0}
	c.events <- StateChange{0, Executing}

	currentTurn := 0

	client, err := rpc.Dial("tcp", "localhost:8080")
	//client, err := rpc.Dial("tcp", "50.17.16.123:8080")

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

	var (
		res   stubs.RunGolResponse
		resMu sync.RWMutex
	)

	done := make(chan struct{})

	// every 2 secs ask broker for alive count
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()

		currentTurn := 0

		for {
			select {
			case <-t.C:
				var alive stubs.AliveCountResponse
				err := client.Call(stubs.GetAliveCountHandler, stubs.AliveCountRequest{}, &alive)
				if err != nil {
					return
				}

				currentTurn++
				c.events <- AliveCellsCount{currentTurn, alive.Count}
				c.events <- TurnComplete{currentTurn}

			case <-done:
				return
			}
		}
	}()

	// keyboard
	go func() {
		paused := false

		for key := range keyPresses {

			switch key {

			case 's':
				snapWorld := world
				snapTurn := 0

				resMu.RLock()
				localRes := res
				resMu.RUnlock()

				if localRes.GolBoard.World != nil {
					snapWorld = localRes.GolBoard.World
					snapTurn = localRes.GolBoard.CurrentTurn
				}

				c.ioCommand <- ioOutput
				name := fmt.Sprintf("%dx%dx%d", p.ImageWidth, p.ImageHeight, snapTurn)
				c.ioFilename <- name

				for y := 0; y < p.ImageHeight; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						c.ioOutput <- snapWorld[y][x]
					}
				}

				c.ioCommand <- ioCheckIdle
				<-c.ioIdle

				c.events <- ImageOutputComplete{snapTurn, name}

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
					c.events <- StateChange{currentTurn, Paused}
				} else {
					paused = false
					c.events <- StateChange{currentTurn, Executing}

				}
			}
		}
	}()
	runtime.Gosched()

	// actually tell broker to run everything
	var tmpRes stubs.RunGolResponse
	err = client.Call(stubs.RunGolHandler, req, &tmpRes)
	if err != nil {
		fmt.Println("RPC error:", err)
		c.events <- StateChange{0, Quitting}
		close(c.events)
		return
	}

	resMu.Lock()
	res = tmpRes
	resMu.Unlock()

	//return final world to server
	finalWorld := res.GolBoard.World
	finalTurn := res.GolBoard.CurrentTurn

	// writing out final pgm
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

	// building list of alive cells for tests
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
