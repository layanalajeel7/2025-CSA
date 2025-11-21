package gol

import (
	"strconv"
	"sync"
	"time"

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

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels, keyPresses <-chan rune) {
	// logical turn count; 0 = initial image
	turn := 0
	// last fully completed turn (i.e. after TurnComplete)
	lastCompletedTurn := 0

	// shared flags (guarded by mu)
	paused := false
	quitFlag := false

	var mu sync.Mutex

	// --- build initial world from input image ---
	world := make([][]uint8, p.ImageHeight)
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth)
	}

	// load initial image
	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	c.ioCommand <- ioInput
	c.ioFilename <- filename

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			val := <-c.ioInput
			world[y][x] = val

			if val == 255 {
				// initial live cells at turn 0
				c.events <- CellFlipped{
					CompletedTurns: 0,
					Cell:           util.Cell{X: x, Y: y},
				}
			}
		}
	}

	// tell SDL/tests that we are now executing (turn 0 state)
	c.events <- StateChange{
		CompletedTurns: lastCompletedTurn,
		NewState:       Executing,
	}

	// helper to count alive cells for a given snapshot
	countAlive := func(w [][]uint8) int {
		total := 0
		for y := 0; y < p.ImageHeight; y++ {
			for x := 0; x < p.ImageWidth; x++ {
				if w[y][x] == 255 {
					total++
				}
			}
		}
		return total
	}

	// snapshot helper: write PGM and send ImageOutputComplete
	outputSnapshot := func(w [][]uint8, t int) {
		name := strconv.Itoa(p.ImageWidth) + "x" +
			strconv.Itoa(p.ImageHeight) + "x" +
			strconv.Itoa(t)

		c.ioCommand <- ioOutput
		c.ioFilename <- name

		for y := 0; y < p.ImageHeight; y++ {
			for x := 0; x < p.ImageWidth; x++ {
				c.ioOutput <- w[y][x]
			}
		}

		c.ioCommand <- ioCheckIdle
		<-c.ioIdle

		c.events <- ImageOutputComplete{
			CompletedTurns: t,
			Filename:       name,
		}
	}

	// --- ticker: AliveCellsCount every 2 seconds based on lastCompletedTurn ---
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			mu.Lock()
			currentTurn := lastCompletedTurn
			wRef := world
			mu.Unlock()

			alive := countAlive(wRef)
			c.events <- AliveCellsCount{
				CompletedTurns: currentTurn,
				CellsCount:     alive,
			}
		}
	}()

	// --- keyboard goroutine: handles 'p', 's', 'q' ---
	go func() {
		for key := range keyPresses {
			switch key {
			case 'p':
				mu.Lock()
				if !paused {
					paused = true
					// report pause at the last *completed* turn
					c.events <- StateChange{
						CompletedTurns: lastCompletedTurn,
						NewState:       Paused,
					}
				} else {
					paused = false
					c.events <- StateChange{
						CompletedTurns: lastCompletedTurn,
						NewState:       Executing,
					}
				}
				mu.Unlock()

			case 's':
				// snapshot current completed state (works both running and paused)
				mu.Lock()
				snapTurn := lastCompletedTurn

				snapWorld := make([][]uint8, p.ImageHeight)
				for y := 0; y < p.ImageHeight; y++ {
					snapWorld[y] = make([]uint8, p.ImageWidth)
					copy(snapWorld[y], world[y])
				}
				mu.Unlock()

				outputSnapshot(snapWorld, snapTurn)

			case 'q':
				mu.Lock()
				quitFlag = true
				mu.Unlock()
			}
		}
	}()

	type chunkResult struct {
		startY  int
		rows    [][]uint8
		flipped []util.Cell
	}

	// --- main simulation loop ---
	for {
		mu.Lock()
		if quitFlag || turn >= p.Turns {
			mu.Unlock()
			break
		}
		isPaused := paused
		currentWorld := world
		mu.Unlock()

		if isPaused {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// choose worker count
		workers := p.Threads
		if workers < 1 {
			workers = 1
		}
		if workers > p.ImageHeight {
			workers = p.ImageHeight
		}

		resultChan := make(chan chunkResult, workers)

		rowsPer := p.ImageHeight / workers
		extra := p.ImageHeight % workers
		startRow := 0

		// launch workers on a snapshot of the current world
		for i := 0; i < workers; i++ {
			size := rowsPer
			if i < extra {
				size++
			}
			endRow := startRow + size

			go func(sY, eY, thisTurn int, worldSnap [][]uint8) {
				outRows := make([][]uint8, eY-sY)
				for i := range outRows {
					outRows[i] = make([]uint8, p.ImageWidth)
				}

				var changed []util.Cell

				for y := sY; y < eY; y++ {
					for x := 0; x < p.ImageWidth; x++ {
						aliveNeighbours := 0
						for dy := -1; dy <= 1; dy++ {
							for dx := -1; dx <= 1; dx++ {
								if dy == 0 && dx == 0 {
									continue
								}
								ny := (y + dy + p.ImageHeight) % p.ImageHeight
								nx := (x + dx + p.ImageWidth) % p.ImageWidth
								if worldSnap[ny][nx] == 255 {
									aliveNeighbours++
								}
							}
						}

						curr := worldSnap[y][x]
						next := curr

						// standard Game of Life rules
						if curr == 255 {
							if aliveNeighbours < 2 || aliveNeighbours > 3 {
								next = 0
							}
						} else {
							if aliveNeighbours == 3 {
								next = 255
							}
						}

						outRows[y-sY][x] = next
						if next != curr {
							changed = append(changed, util.Cell{X: x, Y: y})
						}
					}
				}

				resultChan <- chunkResult{
					startY:  sY,
					rows:    outRows,
					flipped: changed,
				}
			}(startRow, endRow, turn, currentWorld)

			startRow = endRow
		}

		// build the next world from chunks
		newWorld := make([][]uint8, p.ImageHeight)
		for y := range newWorld {
			newWorld[y] = make([]uint8, p.ImageWidth)
		}

		var allFlipped []util.Cell

		for i := 0; i < workers; i++ {
			res := <-resultChan
			for offset, row := range res.rows {
				newWorld[res.startY+offset] = row
			}
			allFlipped = append(allFlipped, res.flipped...)
		}

		// batch CellsFlipped for this turn
		if len(allFlipped) > 0 {
			c.events <- CellsFlipped{
				CompletedTurns: turn + 1,
				Cells:          allFlipped,
			}
		}

		// commit new world & advance turn
		mu.Lock()
		world = newWorld
		turn++
		lastCompletedTurn = turn
		mu.Unlock()

		// TurnComplete for this newly finished turn
		c.events <- TurnComplete{CompletedTurns: lastCompletedTurn}
	}

	// --- final state after loop ---
	mu.Lock()
	finalWorld := world
	finalTurn := lastCompletedTurn
	mu.Unlock()

	var aliveCells []util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if finalWorld[y][x] == 255 {
				aliveCells = append(aliveCells, util.Cell{X: x, Y: y})
			}
		}
	}

	// final snapshot + events
	outputSnapshot(finalWorld, finalTurn)

	c.events <- FinalTurnComplete{
		CompletedTurns: finalTurn,
		Alive:          aliveCells,
	}

	// Make sure IO has finished before quitting
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{
		CompletedTurns: finalTurn,
		NewState:       Quitting,
	}

	close(c.events)
}
