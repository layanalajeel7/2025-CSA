package gol

import (
	"strconv"
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

// distributor runs a clean, single-threaded version of Game of Life.
// No parallel workers here – this is just stage 1 for the distributed part.
// The goal is simply to match the serial behaviour so the tests pass.
func distributor(p Params, c distributorChannels) {

	world := make([][]uint8, p.ImageHeight)
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth) // routine row allocation
	}

	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)

	c.ioCommand <- ioInput
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			val := <-c.ioInput
			world[y][x] = val

			if val == 255 {
				// reporting initial live cells so UI starts consistent
				c.events <- CellFlipped{
					CompletedTurns: 0,
					Cell:           util.Cell{X: x, Y: y},
				}
			}
		}
	}

	// tell UI we’re executing after load
	c.events <- StateChange{
		CompletedTurns: 0,
		NewState:       Executing,
	}

	countNeighbours := func(w [][]uint8, x, y int) int {
		alive := 0
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if dy == 0 && dx == 0 {
					continue // skip itself
				}
				ny := (y + dy + p.ImageHeight) % p.ImageHeight
				nx := (x + dx + p.ImageWidth) % p.ImageWidth
				if w[ny][nx] == 255 {
					alive++
				}
			}
		}
		return alive
	}

	for turn := 0; turn < p.Turns; turn++ {

		// build next world fresh each turn
		newWorld := make([][]uint8, p.ImageHeight)
		for y := range newWorld {
			newWorld[y] = make([]uint8, p.ImageWidth)
		}

		var flipped []util.Cell // track changed cells

		for y := 0; y < p.ImageHeight; y++ {
			for x := 0; x < p.ImageWidth; x++ {

				alive := countNeighbours(world, x, y)
				curr := world[y][x]
				next := curr // default stays the same

				// standard GOL rules
				if curr == 255 {
					if alive < 2 || alive > 3 {
						next = 0
					}
				} else {
					if alive == 3 {
						next = 255
					}
				}

				newWorld[y][x] = next

				if next != curr {
					// store flip with real coords
					flipped = append(flipped, util.Cell{X: x, Y: y})
				}
			}
		}

		// notify UI of flips for this turn
		if len(flipped) > 0 {
			c.events <- CellsFlipped{
				CompletedTurns: turn + 1,
				Cells:          flipped,
			}
		}

		world = newWorld

		// turn complete event
		c.events <- TurnComplete{CompletedTurns: turn + 1}
	}

	finalTurn := p.Turns

	outName := strconv.Itoa(p.ImageWidth) +
		"x" + strconv.Itoa(p.ImageHeight) +
		"x" + strconv.Itoa(finalTurn)

	c.ioCommand <- ioOutput
	c.ioFilename <- outName

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			c.ioOutput <- world[y][x]
		}
	}

	// ensure IO fully finishes
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	// gather final alive cells for final event
	var aliveCells []util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if world[y][x] == 255 {
				aliveCells = append(aliveCells, util.Cell{X: x, Y: y})
			}
		}
	}

	c.events <- FinalTurnComplete{
		CompletedTurns: finalTurn,
		Alive:          aliveCells,
	}

	c.events <- StateChange{
		CompletedTurns: finalTurn,
		NewState:       Quitting,
	}

	close(c.events)
}
