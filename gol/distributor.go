package gol

import (
	"strconv"

	"uk.ac.bris.cs/gameoflife/util"
)

// Channels used between Distributor, IO and TestGol.
type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

// nextGrid runs for a section of rows (startY to endY) and returns that part of the next world.
func nextGrid(p Params, world [][]uint8, startY, endY int) [][]uint8 {
	height := endY - startY
	newWorld := make([][]uint8, height)
	for y := 0; y < height; y++ {
		newWorld[y] = make([]uint8, p.ImageWidth)
	}

	for y := startY; y < endY; y++ {
		for x := 0; x < p.ImageWidth; x++ {

			aliveNeighbours := 0
			for i := -1; i <= 1; i++ {
				for j := -1; j <= 1; j++ {
					if !(i == 0 && j == 0) {
						ny := (y + i + p.ImageHeight) % p.ImageHeight
						nx := (x + j + p.ImageWidth) % p.ImageWidth
						if world[ny][nx] == 255 {
							aliveNeighbours++
						}
					}
				}
			}

			if world[y][x] == 255 {
				if aliveNeighbours < 2 || aliveNeighbours > 3 {
					newWorld[y-startY][x] = 0
				} else {
					newWorld[y-startY][x] = 255
				}
			} else {
				if aliveNeighbours == 3 {
					newWorld[y-startY][x] = 255
				} else {
					newWorld[y-startY][x] = 0
				}
			}
		}
	}
	return newWorld
}

// distributor runs the parallel version of the Game of Life (Stage 2).
func distributor(p Params, c distributorChannels) {

	// 1. Create and read the world from IO.
	world := make([][]uint8, p.ImageHeight)
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth)
	}

	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			world[y][x] = <-c.ioInput
		}
	}

	// 2. Run for the required number of turns using p.Threads workers.
	for turn := 0; turn < p.Turns; turn++ {
		threads := p.Threads
		rowsPerThread := p.ImageHeight / threads

		// create one channel per worker to collect their part of the next world
		results := make([]chan [][]uint8, threads)
		for i := range results {
			results[i] = make(chan [][]uint8)
		}

		// launch each worker goroutine
		for i := 0; i < threads; i++ {
			startY := i * rowsPerThread
			endY := (i + 1) * rowsPerThread
			if i == threads-1 {
				endY = p.ImageHeight
			}

			go func(start, end int, ch chan [][]uint8) {
				section := nextGrid(p, world, start, end)
				ch <- section
			}(startY, endY, results[i])
		}

		// gather results from workers and rebuild full world
		newWorld := make([][]uint8, p.ImageHeight)
		rowIndex := 0
		for i := 0; i < threads; i++ {
			part := <-results[i]
			for _, row := range part {
				newWorld[rowIndex] = row
				rowIndex++
			}
		}

		world = newWorld
	}

	// 3. Collect all live cells at the end.
	var aliveCells []util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if world[y][x] == 255 {
				aliveCells = append(aliveCells, util.Cell{X: x, Y: y})
			}
		}
	}

	// 4. Send final event and finish up.
	c.events <- FinalTurnComplete{CompletedTurns: p.Turns, Alive: aliveCells}
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle
	c.events <- StateChange{CompletedTurns: p.Turns, NewState: Quitting}
	close(c.events)
}
