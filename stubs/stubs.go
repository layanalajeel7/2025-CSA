package stubs

const RunGolHandler = "Broker.RunGol"

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

const GetAliveCountHandler = "Broker.GetAliveCount"

type AliveCountRequest struct{}
type AliveCountResponse struct {
	Count int
}
