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
	Done       Phase = "Done"
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
	Phase           Phase
	RevealOpenIndex int
	Commits         []Commit
	Reveals         []Reveal
	Finalize        *vcv1.JobFinalize
	Verdict         Verdict
	AllRevealed     bool
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

func Commitment(jobID, workerID string, resultHash, nonce []byte) []byte {
	h := sha256.New()
	h.Write([]byte(jobID))
	h.Write([]byte(workerID))
	h.Write(resultHash)
	h.Write(nonce)
	return h.Sum(nil)
}

func Replay(entries []Entry) State {
	state := State{
		Phase: Committing,
	}

	commitByAuthor := make(map[string]Commit)
	revealByAuthor := make(map[string]Reveal)

	for _, e := range entries {
		if e.Envelope == nil || e.Author == "" {
			continue
		}

		switch body := e.Envelope.Body.(type) {
		case *vcv1.Envelope_Announce:
			if state.Announce != nil || body.Announce == nil {
				continue
			}
			ann := body.Announce
			if ann.Replicas == 0 || ann.Quorum == 0 || ann.Quorum > ann.Replicas {
				continue
			}
			jobID := e.Envelope.JobId
			if jobID == "" {
				var err error
				jobID, err = JobID(ann)
				if err != nil {
					continue
				}
			}
			state.JobID = jobID
			state.Submitter = e.Author
			state.Announce = ann
			state.Phase = Committing

		case *vcv1.Envelope_Commit:
			if state.Announce == nil || state.Phase != Committing || body.Commit == nil {
				continue
			}
			if e.Envelope.JobId != "" && e.Envelope.JobId != state.JobID {
				continue
			}
			if len(body.Commit.Commitment) != sha256.Size {
				continue
			}
			if _, exists := commitByAuthor[e.Author]; exists {
				continue
			}

			c := Commit{
				Index:      e.Index,
				Author:     e.Author,
				Commitment: append([]byte(nil), body.Commit.Commitment...),
			}
			commitByAuthor[e.Author] = c
			state.Commits = append(state.Commits, c)

			if uint32(len(state.Commits)) == state.Announce.Replicas {
				state.Phase = Revealing
				state.RevealOpenIndex = e.Index
			}

		case *vcv1.Envelope_Reveal:
			if state.Announce == nil || state.Phase != Revealing || body.Reveal == nil {
				continue
			}
			if e.Envelope.JobId != "" && e.Envelope.JobId != state.JobID {
				continue
			}
			if state.Announce.RevealWindow > 0 && e.Index-state.RevealOpenIndex > int(state.Announce.RevealWindow) {
				continue
			}
			c, counted := commitByAuthor[e.Author]
			if !counted {
				continue
			}
			if _, alreadyRevealed := revealByAuthor[e.Author]; alreadyRevealed {
				continue
			}
			rev := body.Reveal
			if len(rev.ResultHash) != sha256.Size || rev.ResultCid == "" {
				continue
			}
			expected := Commitment(state.JobID, e.Author, rev.ResultHash, rev.Nonce)
			if !bytes.Equal(expected, c.Commitment) {
				continue
			}

			r := Reveal{
				Index:      e.Index,
				Author:     e.Author,
				ResultHash: append([]byte(nil), rev.ResultHash...),
				ResultCID:  rev.ResultCid,
				Nonce:      append([]byte(nil), rev.Nonce...),
				ExitCode:   rev.ExitCode,
			}
			revealByAuthor[e.Author] = r
			state.Reveals = append(state.Reveals, r)

		case *vcv1.Envelope_Finalize:
			if state.Announce == nil || state.Phase != Revealing || body.Finalize == nil {
				continue
			}
			if e.Envelope.JobId != "" && e.Envelope.JobId != state.JobID {
				continue
			}
			if e.Author != state.Submitter {
				continue
			}
			state.Finalize = body.Finalize
			state.Phase = Done
		}
	}

	if state.Announce != nil {
		state.Verdict = Tally(state.Commits, state.Reveals, int(state.Announce.Quorum))
		state.AllRevealed = uint32(len(state.Commits)) == state.Announce.Replicas &&
			len(state.Reveals) == len(state.Commits)
	}

	return state
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
