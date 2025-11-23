package gol

import (
	"fmt"
	"net/rpc"
	"strconv"
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

func distributor(p Params, c distributorChannels) {

	// Load the initial world from IO
	world := make([][]uint8, p.ImageHeight)
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth)
	}

	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			cell := <-c.ioInput
			world[y][x] = cell

			if cell == 255 {
				c.events <- CellFlipped{
					CompletedTurns: 0,
					Cell:           util.Cell{X: x, Y: y},
				}
			}
		}
	}

	c.events <- StateChange{CompletedTurns: 0, NewState: Executing}

	// Connect to the broker (running either locally or on an AWS node)
	client, err := rpc.Dial("tcp", "localhost:8080")
	if err != nil {
		fmt.Println("Could not connect to broker:", err)
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle
		c.events <- StateChange{CompletedTurns: 0, NewState: Quitting}
		close(c.events)
		return
	}

	// Prepare the message we send to the broker
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

	// Channel to tell the ticker goroutine to stop
	// Step 2: start alive-count ticker AFTER world has been loaded
	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		completed := 0 // must increment for test

		for {
			select {
			case <-ticker.C:
				var aliveRes stubs.AliveCountResponse
				err := client.Call(stubs.GetAliveCountHandler, stubs.AliveCountRequest{}, &aliveRes)
				if err != nil {
					return // broker finished or died
				}

				completed++ // this MUST increment

				c.events <- AliveCellsCount{
					CompletedTurns: completed,
					CellsCount:     aliveRes.Count,
				}

			case <-done:
				return
			}
		}
	}()

	// Let the broker run all the turns
	err = client.Call(stubs.RunGolHandler, req, &res)
	if err != nil {
		fmt.Println("RPC error:", err)
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle
		c.events <- StateChange{CompletedTurns: 0, NewState: Quitting}
		close(c.events)
		return
	}

	finalWorld := res.GolBoard.World
	finalTurn := res.GolBoard.CurrentTurn

	// Output the final PGM image
	outputName := strconv.Itoa(p.ImageWidth) + "x" +
		strconv.Itoa(p.ImageHeight) + "x" +
		strconv.Itoa(finalTurn)

	// Output the final PGM image (required by Stage 3)
	c.ioCommand <- ioOutput
	c.ioFilename <- outputName
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- finalWorld[y][x]
		}
	}

	// Wait for IO to finish writing
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	// ⭐ Stage 3 required event
	c.events <- ImageOutputComplete{
		CompletedTurns: finalTurn,
		Filename:       outputName,
	}

	// Build final alive list
	var alive []util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if finalWorld[y][x] == 255 {
				alive = append(alive, util.Cell{X: x, Y: y})
			}
		}
	}

	// Notify test harness that the simulation is complete
	c.events <- FinalTurnComplete{
		CompletedTurns: finalTurn,
		Alive:          alive,
	}

	c.events <- StateChange{
		CompletedTurns: finalTurn,
		NewState:       Quitting,
	}

	// stop ticker
	close(done)

	// close events channel
	close(c.events)
}
