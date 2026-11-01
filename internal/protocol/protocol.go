package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
	"google.golang.org/protobuf/proto"
)

type Phase string

const (
	Committing Phase = "Committing"
	Revealing  Phase = "Revealing"
	Accepted   Phase = "Accepted"
	Expired    Phase = "Expired"
	Done       Phase = Accepted
)

type Entry struct {
	Index    int
	Author   string
	Envelope *vcv1.Envelope
}

type Commit struct {
	Index      int
	Author     string
	Commitment []byte
}

type Reveal struct {
	Index      int
	Author     string
	ResultHash []byte
	ResultCID  string
	Nonce      []byte
	ExitCode   int32
}

type Verdict struct {
	Status       Phase
	HasQuorum    bool
	AcceptedHash []byte
	WinnerCID    string
	Winners      []string
	Divergent    []string
	NoReveal     []string
}

type State struct {
	JobID           string
	Submitter       string
	Announce        *vcv1.JobAnnounce
	AnnounceIndex   int
	Phase           Phase
	RevealOpenIndex int
	LastIndex       int
	Commits         []Commit
	Reveals         []Reveal
	Finalize        *vcv1.JobFinalize
	FinalizedBy     string
	Verdict         Verdict
	AllRevealed     bool
	Finalizable     bool
}

func JobID(announce *vcv1.JobAnnounce) (string, error) {
	if announce == nil {
		return "", fmt.Errorf("announce cannot be nil")
	}
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(announce)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Null separators prevent length-extension ambiguity between variable-length strings.
func Commitment(jobID, workerID string, resultHash, nonce []byte) []byte {
	h := sha256.New()
	h.Write([]byte(jobID))
	h.Write([]byte{0})
	h.Write([]byte(workerID))
	h.Write([]byte{0})
	h.Write(resultHash)
	h.Write(nonce)
	return h.Sum(nil)
}

func Replay(entries []Entry) State {
	state := State{
		Phase:     Committing,
		LastIndex: -1,
	}
	for _, e := range entries {
		state = Apply(state, e)
	}
	return state
}

// Deadlines are evaluated against chain indices rather than wall-clock time so
// every node derives the exact same phase and verdict from consensus order.
func Apply(state State, e Entry) State {
	if state.Phase == "" {
		state.Phase = Committing
		if state.Announce == nil {
			state.LastIndex = -1
		}
	}
	if e.Envelope == nil || e.Author == "" {
		return state
	}

	state.Commits = append([]Commit(nil), state.Commits...)
	state.Reveals = append([]Reveal(nil), state.Reveals...)

	if state.Announce == nil {
		body, ok := e.Envelope.Body.(*vcv1.Envelope_Announce)
		if !ok || body.Announce == nil {
			return state
		}
		ann := body.Announce
		if ann.Replicas < 1 || ann.Quorum < 1 || ann.Quorum > ann.Replicas || ann.CommitWindow == 0 || ann.RevealWindow == 0 {
			return state
		}
		jobID := e.Envelope.JobId
		if jobID == "" {
			var err error
			jobID, err = JobID(ann)
			if err != nil {
				return state
			}
		}
		state.JobID = jobID
		state.Submitter = e.Author
		state.Announce = ann
		state.AnnounceIndex = e.Index
		state.LastIndex = e.Index
		state.Phase = Committing
		return state
	}

	if e.Index <= state.LastIndex {
		return state
	}
	if !matchesJobID(state.JobID, e.Envelope) {
		return state
	}
	state.LastIndex = e.Index

	if state.Finalize != nil {
		return state
	}

	if state.Phase == Committing && e.Index-state.AnnounceIndex > int(state.Announce.CommitWindow) {
		if uint32(len(state.Commits)) >= state.Announce.Quorum {
			state.Phase = Revealing
			state.RevealOpenIndex = e.Index
			state = recomputeVerdict(state)
		} else {
			state.Phase = Expired
			state.Finalizable = true
			state = recomputeVerdict(state)
		}
	}

	if state.Phase == Revealing && e.Index-state.RevealOpenIndex > int(state.Announce.RevealWindow) {
		state.Finalizable = true
		state = recomputeVerdict(state)
		if !state.Verdict.HasQuorum {
			state.Phase = Expired
		}
	}

	switch body := e.Envelope.Body.(type) {
	case *vcv1.Envelope_Commit:
		if state.Phase != Committing || body.Commit == nil {
			return state
		}
		if len(body.Commit.Commitment) != sha256.Size {
			return state
		}
		if _, exists := findCommit(state.Commits, e.Author); exists {
			return state
		}

		c := Commit{
			Index:      e.Index,
			Author:     e.Author,
			Commitment: append([]byte(nil), body.Commit.Commitment...),
		}
		state.Commits = append(state.Commits, c)

		if uint32(len(state.Commits)) == state.Announce.Replicas {
			state.Phase = Revealing
			state.RevealOpenIndex = e.Index
		}
		state = recomputeVerdict(state)

	case *vcv1.Envelope_Reveal:
		if state.Phase != Revealing || state.Finalizable || body.Reveal == nil {
			return state
		}
		if e.Index-state.RevealOpenIndex > int(state.Announce.RevealWindow) {
			return state
		}
		c, counted := findCommit(state.Commits, e.Author)
		if !counted || hasReveal(state.Reveals, e.Author) {
			return state
		}
		rev := body.Reveal
		if len(rev.ResultHash) != sha256.Size || rev.ResultCid == "" {
			return state
		}
		expected := Commitment(state.JobID, e.Author, rev.ResultHash, rev.Nonce)
		if !bytes.Equal(expected, c.Commitment) {
			return state
		}

		r := Reveal{
			Index:      e.Index,
			Author:     e.Author,
			ResultHash: append([]byte(nil), rev.ResultHash...),
			ResultCID:  rev.ResultCid,
			Nonce:      append([]byte(nil), rev.Nonce...),
			ExitCode:   rev.ExitCode,
		}
		state.Reveals = append(state.Reveals, r)

		if len(state.Reveals) == len(state.Commits) {
			state.AllRevealed = true
			state.Finalizable = true
		}
		state = recomputeVerdict(state)
		if state.Finalizable && !state.Verdict.HasQuorum {
			state.Phase = Expired
		}

	case *vcv1.Envelope_Finalize:
		if !VerifyFinalize(state, body.Finalize) {
			return state
		}
		state.Finalize = cloneFinalize(body.Finalize)
		state.FinalizedBy = e.Author
		if state.Verdict.HasQuorum {
			state.Phase = Accepted
		} else {
			state.Phase = Expired
		}

	case *vcv1.Envelope_Tick:
		// Tick advances chain index only; window transitions above already ran.
	}

	return state
}

// Any node may post JobFinalize once the state is finalizable because the verdict
// is a deterministic function of prior chain entries and is verified here.
func VerifyFinalize(state State, finalize *vcv1.JobFinalize) bool {
	if finalize == nil || state.Announce == nil || !state.Finalizable {
		return false
	}
	v := state.Verdict
	if !bytes.Equal(finalize.AcceptedHash, v.AcceptedHash) {
		return false
	}
	if !slicesEqual(finalize.Winners, v.Winners) {
		return false
	}
	if !slicesEqual(finalize.Divergent, v.Divergent) {
		return false
	}
	if !slicesEqual(finalize.NoReveal, v.NoReveal) {
		return false
	}
	return true
}

func Tally(commits []Commit, reveals []Reveal, quorum int) Verdict {
	var v Verdict
	if quorum <= 0 || len(commits) == 0 {
		return v
	}

	revealByAuthor := make(map[string]Reveal, len(reveals))
	type hashGroup struct {
		hash    []byte
		cid     string
		members []string
	}
	var groups []*hashGroup
	groupByHex := make(map[string]*hashGroup)

	for _, r := range reveals {
		revealByAuthor[r.Author] = r
		key := hex.EncodeToString(r.ResultHash)
		g, ok := groupByHex[key]
		if !ok {
			g = &hashGroup{
				hash: append([]byte(nil), r.ResultHash...),
				cid:  r.ResultCID,
			}
			groupByHex[key] = g
			groups = append(groups, g)
		}
		g.members = append(g.members, r.Author)
	}

	var winningGroup *hashGroup
	for _, g := range groups {
		if len(g.members) >= quorum {
			if winningGroup == nil || len(g.members) > len(winningGroup.members) {
				winningGroup = g
			}
		}
	}

	if winningGroup == nil {
		for _, c := range commits {
			if _, ok := revealByAuthor[c.Author]; !ok {
				v.NoReveal = append(v.NoReveal, c.Author)
			} else {
				v.Divergent = append(v.Divergent, c.Author)
			}
		}
		return v
	}

	v.HasQuorum = true
	v.AcceptedHash = append([]byte(nil), winningGroup.hash...)
	v.WinnerCID = winningGroup.cid

	for _, c := range commits {
		r, ok := revealByAuthor[c.Author]
		if !ok {
			v.NoReveal = append(v.NoReveal, c.Author)
			continue
		}
		if bytes.Equal(r.ResultHash, v.AcceptedHash) {
			v.Winners = append(v.Winners, c.Author)
		} else {
			v.Divergent = append(v.Divergent, c.Author)
		}
	}

	return v
}

func recomputeVerdict(state State) State {
	if state.Announce == nil {
		return state
	}
	v := Tally(state.Commits, state.Reveals, int(state.Announce.Quorum))
	if state.Finalizable {
		if v.HasQuorum {
			v.Status = Accepted
		} else {
			v.Status = Expired
		}
	}
	state.Verdict = v
	return state
}

func matchesJobID(expected string, env *vcv1.Envelope) bool {
	jobID := env.JobId
	if t, ok := env.Body.(*vcv1.Envelope_Tick); ok {
		if t.Tick == nil {
			return false
		}
		if jobID == "" {
			jobID = t.Tick.JobId
		} else if t.Tick.JobId != "" && t.Tick.JobId != jobID {
			return false
		}
	}
	if jobID != "" && jobID != expected {
		return false
	}
	return true
}

func findCommit(commits []Commit, author string) (Commit, bool) {
	for _, c := range commits {
		if c.Author == author {
			return c, true
		}
	}
	return Commit{}, false
}

func hasReveal(reveals []Reveal, author string) bool {
	for _, r := range reveals {
		if r.Author == author {
			return true
		}
	}
	return false
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func cloneFinalize(f *vcv1.JobFinalize) *vcv1.JobFinalize {
	if f == nil {
		return nil
	}
	return &vcv1.JobFinalize{
		AcceptedHash: append([]byte(nil), f.AcceptedHash...),
		Winners:      append([]string(nil), f.Winners...),
		Divergent:    append([]string(nil), f.Divergent...),
		NoReveal:     append([]string(nil), f.NoReveal...),
	}
}
