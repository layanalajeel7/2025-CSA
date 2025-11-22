package stubs

// RPC function names (kept short)
const RunGolHandler = "Broker.RunGol"

// structure passed to the worker node
type GolBoard struct {
	World       [][]uint8
	Width       int
	Height      int
	CurrentTurn int
}

type RunGolRequest struct {
	GolBoard GolBoard
	Turns    int
	Threads  int
}

type RunGolResponse struct {
	GolBoard GolBoard
}
