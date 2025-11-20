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

	turn := 0 //created new var turn and assigned value 0
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

		// simple read-only accessor so goroutines don't touch world directly
		get := func(y, x int) uint8 {
			return world[y][x]
		}

		// number of workers to use
		workers := p.Threads
		if workers < 1 {
			workers = 1
		}
		if workers > p.ImageHeight {
			workers = p.ImageHeight
		}

		// worker output container
		type chunkResult struct {
			startY  int
			rows    [][]uint8
			flipped []util.Cell
		}

		resultChan := make(chan chunkResult, workers)

		// split rows evenly across workers
		rowsPer := p.ImageHeight / workers
		extra := p.ImageHeight % workers

		start := 0

		// launch workers
		for i := 0; i < workers; i++ {

			size := rowsPer
			if i < extra {
				size++ // handle leftover rows
			}

			end := start + size

			go func(sY, eY, thisTurn int) {

				// allocate chunk
				outRows := make([][]uint8, eY-sY)
				for i := range outRows {
					outRows[i] = make([]uint8, p.ImageWidth)
				}

				var changed []util.Cell

				for y := sY; y < eY; y++ {
					for x := 0; x < p.ImageWidth; x++ {

						// neighbor count
						alive := 0
						for dy := -1; dy <= 1; dy++ {
							for dx := -1; dx <= 1; dx++ {
								if dy == 0 && dx == 0 {
									continue
								}
								ny := (y + dy + p.ImageHeight) % p.ImageHeight
								nx := (x + dx + p.ImageWidth) % p.ImageWidth
								if get(ny, nx) == 255 {
									alive++
								}
							}
						}

						curr := get(y, x)
						next := curr

						// apply GoL rules
						if curr == 255 {
							if alive < 2 || alive > 3 {
								next = 0
							}
						} else {
							if alive == 3 {
								next = 255
							}
						}

						outRows[y-sY][x] = next

						// record flips
						if next != curr {
							changed = append(changed, util.Cell{X: x, Y: y})
						}
					}
				}

				resultChan <- chunkResult{sY, outRows, changed}

			}(start, end, turn)

			start = end
		}

		// construct next world
		newWorld := make([][]uint8, p.ImageHeight)
		for y := range newWorld {
			newWorld[y] = make([]uint8, p.ImageWidth)
		}

		var allFlipped []util.Cell

		for i := 0; i < workers; i++ {
			res := <-resultChan

			// copy worker rows
			for offset, row := range res.rows {
				newWorld[res.startY+offset] = row
			}

			allFlipped = append(allFlipped, res.flipped...)
		}

		// send CellFlipped events
		for _, cell := range allFlipped {
			c.events <- CellFlipped{
				CompletedTurns: turn + 1,
				Cell:           cell,
			}
		}

		// move to next world
		world = newWorld
		turn++

		// completed turn
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
