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

func distributor(p Params, c distributorChannels) {

	turn := 0
	c.events <- StateChange{turn, Executing}
	world := make([][]uint8, p.ImageHeight)
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth)
	}

	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			val := <-c.ioInput
			world[y][x] = val

			if val == 255 {
				c.events <- CellFlipped{
					CompletedTurns: 0,
					Cell:           util.Cell{X: x, Y: y},
				}
			}
		}
	}

	for turn < p.Turns {

		newWorld := make([][]uint8, p.ImageHeight)
		for y := range newWorld {
			newWorld[y] = make([]uint8, p.ImageWidth)
		}

		for y := 0; y < p.ImageHeight; y++ {
			for x := 0; x < p.ImageWidth; x++ {

				aliveNeighbours := 0
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						if dy == 0 && dx == 0 {
							continue
						}

						ny := (y + dy + p.ImageHeight) % p.ImageHeight
						nx := (x + dx + p.ImageWidth) % p.ImageWidth

						if world[ny][nx] == 255 {
							aliveNeighbours++
						}
					}
				}
				// current state
				current := world[y][x]
				next := current

				if current == 255 {
					if aliveNeighbours < 2 || aliveNeighbours > 3 {
						next = 0
					}
				} else {
					if aliveNeighbours == 3 {
						next = 255
					}
				}

				newWorld[y][x] = next
				if next != current {
					c.events <- CellFlipped{
						CompletedTurns: turn + 1,
						Cell:           util.Cell{X: x, Y: y},
					}
				}
			}
		}
		// move to next world
		world = newWorld
		turn++

		// turn complete (SDL draws a frame)
		c.events <- TurnComplete{CompletedTurns: turn}
	}
	var alive []util.Cell

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if world[y][x] == 255 {
				alive = append(alive, util.Cell{X: x, Y: y})
			}
		}
	}
	c.events <- FinalTurnComplete{
		CompletedTurns: turn,
		Alive:          alive,
	}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{CompletedTurns: turn, NewState: Quitting}
	close(c.events)
}
