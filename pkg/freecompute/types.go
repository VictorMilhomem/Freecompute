package freecompute

const (
	DefaultCommitWindow uint32 = 20
	DefaultRevealWindow uint32 = 20
	DefaultPostCost     int64  = 500
	DefaultMaxTicks     int    = 50
)

type inputKind int

const (
	inputUnset inputKind = iota
	inputFile
	inputDir
	inputBytes
	inputCID
)

type Input struct {
	kind inputKind
	path string
	name string
	data []byte
	cid  string
}

func FromFile(path string) Input {
	return Input{kind: inputFile, path: path}
}

func FromDir(path string) Input {
	return Input{kind: inputDir, path: path}
}

func FromBytes(name string, data []byte) Input {
	return Input{kind: inputBytes, name: name, data: append([]byte(nil), data...)}
}

func FromCID(cid string) Input {
	return Input{kind: inputCID, cid: cid}
}

type JobSpec struct {
	Image                      string
	Cmd                        []string
	Input                      Input
	Replicas, Quorum           uint32
	Reward                     int64
	CommitWindow, RevealWindow uint32
}

type Result struct {
	JobID, AcceptedHash, OutputCID string
	Winners, Divergent, NoReveal   []string
}

type Event interface {
	isEvent()
}

type Announced struct {
	JobID string
}

func (Announced) isEvent() {}

type Committed struct {
	JobID  string
	Worker string
}

func (Committed) isEvent() {}

type RevealPhaseOpened struct {
	JobID string
}

func (RevealPhaseOpened) isEvent() {}

type Revealed struct {
	JobID  string
	Worker string
}

func (Revealed) isEvent() {}

type Finalized struct {
	Result Result
}

func (Finalized) isEvent() {}

type Expired struct {
	JobID string
}

func (Expired) isEvent() {}
