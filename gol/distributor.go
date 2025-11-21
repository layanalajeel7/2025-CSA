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
	turn := 0              // starting at turn zero consistent
	lastCompletedTurn := 0 // tracking fully finished turns so events stay clean

	paused := false   // pause flag so we don’t run ahead when user pauses
	quitFlag := false // quit flag simple exit path

	var mu sync.Mutex // lock for shared state

	// build initial world
	world := make([][]uint8, p.ImageHeight) // allocating the outer slice first
	for y := range world {
		world[y] = make([]uint8, p.ImageWidth) // then each row, a bit routine but necessary
	}

	// load initial image
	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight) // preparing filename
	c.ioCommand <- ioInput
	c.ioFilename <- filename // ask IO to load the initial grid

	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			val := <-c.ioInput // reading the initial input one cell at a time
			world[y][x] = val  // setting it directly

			if val == 255 {
				// reporting live cells at turn 0 to keep the viewer fully in sync
				c.events <- CellFlipped{
					CompletedTurns: 0,
					Cell:           util.Cell{X: x, Y: y},
				}
			}
		}
	}

	// initial state tell UI we’re actually executing now after loading
	c.events <- StateChange{
		CompletedTurns: lastCompletedTurn,
		NewState:       Executing,
	}

	// helper count alive
	countAlive := func(w [][]uint8) int {
		total := 0
		for y := 0; y < p.ImageHeight; y++ {
			for x := 0; x < p.ImageWidth; x++ {
				if w[y][x] == 255 {
					total++ // scan
				}
			}
		}
		return total
	}

	// snapshot helper
	outputSnapshot := func(w [][]uint8, t int) {
		// constructing the snapshot filename with turn index just so everything stays nicely labeled
		name := strconv.Itoa(p.ImageWidth) + "x" +
			strconv.Itoa(p.ImageHeight) + "x" +
			strconv.Itoa(t)

		c.ioCommand <- ioOutput
		c.ioFilename <- name // instruct IO where to write

		for y := 0; y < p.ImageHeight; y++ {
			for x := 0; x < p.ImageWidth; x++ {
				// writing the entire grid out (repetitive)
				c.ioOutput <- w[y][x]
			}
		}

		// making sure IO finishes before sending the event
		c.ioCommand <- ioCheckIdle
		<-c.ioIdle

		c.events <- ImageOutputComplete{
			CompletedTurns: t,
			Filename:       name,
		}
	}

	// ticker periodic alive count
	ticker := time.NewTicker(2 * time.Second) // regular ping to keep UI updated
	defer ticker.Stop()

	go func() {
		for range ticker.C {
			mu.Lock()
			currentTurn := lastCompletedTurn // snapshot the turn
			wRef := world                    // shallow reference is enough here
			mu.Unlock()

			alive := countAlive(wRef)
			c.events <- AliveCellsCount{
				CompletedTurns: currentTurn,
				CellsCount:     alive, // just reporting the count regularly
			}
		}
	}()

	// key handler goroutine
	go func() {
		for key := range keyPresses {
			switch key {
			case 'p': // toggle pause
				mu.Lock()
				if !paused {
					paused = true
					// letting UI know we paused exactly at the last completed turn
					c.events <- StateChange{
						CompletedTurns: lastCompletedTurn,
						NewState:       Paused,
					}
				} else {
					paused = false
					// going back into executing
					c.events <- StateChange{
						CompletedTurns: lastCompletedTurn,
						NewState:       Executing,
					}
				}
				mu.Unlock()

			case 's': // snapshot
				mu.Lock()
				snapTurn := lastCompletedTurn // snapshot is always from the last completed turn

				// making an actual copy so the snapshot is consistent even if world updates immediately after
				snapWorld := make([][]uint8, p.ImageHeight)
				for y := 0; y < p.ImageHeight; y++ {
					snapWorld[y] = make([]uint8, p.ImageWidth)
					copy(snapWorld[y], world[y]) // copying row by row
				}
				mu.Unlock()

				outputSnapshot(snapWorld, snapTurn)

			case 'q': // quit
				mu.Lock()
				// setting the quit flag so the main loop can handle a clean exit
				quitFlag = true
				mu.Unlock()
			}
		}
	}()

	type chunkResult struct {
		startY  int
		rows    [][]uint8
		flipped []util.Cell // collecting changed cells just to keep UI synced per turn
	}

	// main simulation loop
	for {
		mu.Lock()
		if quitFlag || turn >= p.Turns {
			mu.Unlock()
			break // breaking out here so all cleanup happens afterward
		}
		isPaused := paused
		currentWorld := world // snapshot pointer for workers to avoid weird concurrent reads
		mu.Unlock()

		if isPaused {
			// small sleep to avoid burning CPU while paused, nothing dramatic
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// choose worker count
		workers := p.Threads
		if workers < 1 {
			workers = 1 // fallback so we never hit zero workers
		}
		if workers > p.ImageHeight {
			workers = p.ImageHeight // avoid more workers than rows, just simpler
		}

		resultChan := make(chan chunkResult, workers)

		rowsPer := p.ImageHeight / workers
		extra := p.ImageHeight % workers // distributing the leftover rows evenly
		startRow := 0

		// spawn workers
		for i := 0; i < workers; i++ {
			size := rowsPer
			if i < extra {
				size++ // giving the first few workers the extra rows
			}
			endRow := startRow + size

			go func(sY, eY, thisTurn int, worldSnap [][]uint8) {
				// preparing output rows
				outRows := make([][]uint8, eY-sY)
				for i := range outRows {
					outRows[i] = make([]uint8, p.ImageWidth)
				}

				var changed []util.Cell // tracking which cells changed this turn

				// performing the actual gol update logic on this worker’s slice
				for y := sY; y < eY; y++ {
					for x := 0; x < p.ImageWidth; x++ {

						aliveNeighbours := 0
						// scanning neighbours with wrapping
						for dy := -1; dy <= 1; dy++ {
							for dx := -1; dx <= 1; dx++ {
								if dy == 0 && dx == 0 {
									continue // skipping the cell itself
								}
								ny := (y + dy + p.ImageHeight) % p.ImageHeight // wrapping around because torus grid
								nx := (x + dx + p.ImageWidth) % p.ImageWidth
								if worldSnap[ny][nx] == 255 {
									aliveNeighbours++
								}
							}
						}

						curr := worldSnap[y][x]
						next := curr // default stays the same unless rules change it

						// standard rules
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
							// recording changed cells so UI can highlight updates efficiently
							changed = append(changed, util.Cell{X: x, Y: y})
						}
					}
				}

				// reporting this worker's results back so they can be merged cleanly
				resultChan <- chunkResult{
					startY:  sY,
					rows:    outRows,
					flipped: changed,
				}
			}(startRow, endRow, turn, currentWorld)

			startRow = endRow // updating starting row for next worker
		}

		// assemble new world
		newWorld := make([][]uint8, p.ImageHeight) // allocating full new world
		for y := range newWorld {
			newWorld[y] = make([]uint8, p.ImageWidth) // same layout as original world
		}

		var allFlipped []util.Cell // collect flips from all workers

		for i := 0; i < workers; i++ {
			res := <-resultChan
			// putting the rows back in place
			for offset, row := range res.rows {
				newWorld[res.startY+offset] = row
			}
			allFlipped = append(allFlipped, res.flipped...)
		}

		// flipping event
		if len(allFlipped) > 0 {
			// reporting flips once per turn keeps event spam down and stays consistent with spec
			c.events <- CellsFlipped{
				CompletedTurns: turn + 1,
				Cells:          allFlipped,
			}
		}

		// update world
		mu.Lock()
		world = newWorld // commit next world so workers use the correct snapshot
		turn++           // advance turn
		lastCompletedTurn = turn
		mu.Unlock()

		// report turn completion
		c.events <- TurnComplete{CompletedTurns: lastCompletedTurn}
	}

	// final state
	mu.Lock()
	finalWorld := world            // snapshot final state
	finalTurn := lastCompletedTurn // last legitimate completed turn
	mu.Unlock()

	var aliveCells []util.Cell
	for y := 0; y < p.ImageHeight; y++ {
		for x := 0; x < p.ImageWidth; x++ {
			if finalWorld[y][x] == 255 {
				// gathering final set of live cells for the final event
				aliveCells = append(aliveCells, util.Cell{X: x, Y: y})
			}
		}
	}

	// final snapshot
	outputSnapshot(finalWorld, finalTurn) // this gives tests/UI their last image

	c.events <- FinalTurnComplete{
		CompletedTurns: finalTurn,
		Alive:          aliveCells, // sending the final list of live cells
	}

	// wait for IO
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle // ensuring IO is really finished before shutdown

	c.events <- StateChange{
		CompletedTurns: finalTurn,
		NewState:       Quitting, // clean shutdown signal
	}

	close(c.events) // end events so everything downstream closes properly
}
