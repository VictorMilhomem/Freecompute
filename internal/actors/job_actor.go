package actors

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/internal/canon"
	"github.com/VictorMilhomem/freecompute/internal/protocol"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

var ErrJobExpiredNoQuorum = errors.New("job expired without quorum")

type JobResult struct {
	JobID        string
	AcceptedHash string
	OutputCID    string
	Winners      []string
	Divergent    []string
	NoReveal     []string
}

type JobEventKind int

const (
	EventAnnounced JobEventKind = iota + 1
	EventCommitted
	EventRevealPhaseOpened
	EventRevealed
	EventFinalized
	EventExpired
)

type JobEvent struct {
	Kind   JobEventKind
	JobID  string
	Worker string
	Result JobResult
}

type JobActorConfig struct {
	NodeID    string
	PublicKey string
	JobID     string
	Announce  *vcv1.JobAnnounce
	Bridge    gen.Atom
	Store     Store
	Logger    *slog.Logger
	OnEvent   func(JobEvent)
	OnDone    func(JobResult, error)
}

type publishAnnounceMsg struct{}

type JobActor struct {
	act.Actor
	cfg               JobActorConfig
	announced         bool
	emittedAnnounce   bool
	emittedCommits    map[string]bool
	emittedRevealOpen bool
	emittedReveals    map[string]bool
	finalizePosted    bool
	completed         bool
}

func NewJobActor(cfg JobActorConfig) *JobActor {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &JobActor{
		cfg:            cfg,
		emittedCommits: make(map[string]bool),
		emittedReveals: make(map[string]bool),
	}
}

func (j *JobActor) Init(args ...any) error {
	_, _ = j.SendAfter(j.PID(), publishAnnounceMsg{}, 5*time.Millisecond)
	return nil
}

func (j *JobActor) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case publishAnnounceMsg:
		if j.announced || j.completed {
			return nil
		}
		env := &vcv1.Envelope{
			Version: 1,
			JobId:   j.cfg.JobID,
			Body: &vcv1.Envelope_Announce{
				Announce: j.cfg.Announce,
			},
		}
		respAny, err := j.Call(j.cfg.Bridge, PublishReq{Envelope: env})
		if err != nil {
			j.finish(JobResult{JobID: j.cfg.JobID}, err)
			return gen.TerminateReasonNormal
		}
		if resp, ok := respAny.(PublishResp); ok && resp.Err != nil {
			j.finish(JobResult{JobID: j.cfg.JobID}, resp.Err)
			return gen.TerminateReasonNormal
		}
		j.announced = true

	case JobStateUpdate:
		return j.onStateUpdate(msg.State)
	}
	return nil
}

func (j *JobActor) onStateUpdate(st protocol.State) error {
	if j.completed || st.Announce == nil {
		return nil
	}
	j.announced = true

	if !j.emittedAnnounce {
		j.emittedAnnounce = true
		j.emit(JobEvent{Kind: EventAnnounced, JobID: st.JobID})
	}

	for _, c := range st.Commits {
		if !j.emittedCommits[c.Author] {
			j.emittedCommits[c.Author] = true
			j.emit(JobEvent{Kind: EventCommitted, JobID: st.JobID, Worker: c.Author})
		}
	}

	if (st.Phase == protocol.Revealing || st.RevealOpenIndex > 0) && !j.emittedRevealOpen && len(st.Commits) > 0 {
		j.emittedRevealOpen = true
		j.emit(JobEvent{Kind: EventRevealPhaseOpened, JobID: st.JobID})
	}

	for _, r := range st.Reveals {
		if !j.emittedReveals[r.Author] {
			j.emittedReveals[r.Author] = true
			j.emit(JobEvent{Kind: EventRevealed, JobID: st.JobID, Worker: r.Author})
		}
	}

	if st.Finalizable && st.Finalize == nil && !j.finalizePosted {
		j.finalizePosted = true
		finEnv := &vcv1.Envelope{
			Version: 1,
			JobId:   st.JobID,
			Body: &vcv1.Envelope_Finalize{
				Finalize: &vcv1.JobFinalize{
					AcceptedHash: st.Verdict.AcceptedHash,
					Winners:      st.Verdict.Winners,
					Divergent:    st.Verdict.Divergent,
					NoReveal:     st.Verdict.NoReveal,
				},
			},
		}
		_, _ = j.Call(j.cfg.Bridge, PublishReq{Envelope: finEnv})
		return nil
	}

	if st.Finalize == nil {
		return nil
	}

	j.completed = true
	reward := int64(st.Announce.RewardReps)

	if st.Verdict.HasQuorum {
		if err := j.verifyWinnerDigest(st.Verdict.WinnerCID, st.Verdict.AcceptedHash); err != nil {
			j.finish(JobResult{JobID: st.JobID}, err)
			return gen.TerminateReasonNormal
		}
		if reward > 0 {
			for _, w := range st.Verdict.Winners {
				j.vote(true, reward, w)
			}
			for _, d := range st.Verdict.Divergent {
				j.vote(false, reward, d)
			}
			for _, nr := range st.Verdict.NoReveal {
				j.vote(false, reward, nr)
			}
		}
		res := JobResult{
			JobID:        st.JobID,
			AcceptedHash: hex.EncodeToString(st.Verdict.AcceptedHash),
			OutputCID:    st.Verdict.WinnerCID,
			Winners:      append([]string(nil), st.Verdict.Winners...),
			Divergent:    append([]string(nil), st.Verdict.Divergent...),
			NoReveal:     append([]string(nil), st.Verdict.NoReveal...),
		}
		j.emit(JobEvent{Kind: EventFinalized, JobID: st.JobID, Result: res})
		j.finish(res, nil)
		return gen.TerminateReasonNormal
	}

	if reward > 0 {
		for _, d := range st.Verdict.Divergent {
			j.vote(false, reward, d)
		}
		for _, nr := range st.Verdict.NoReveal {
			j.vote(false, reward, nr)
		}
	}
	res := JobResult{
		JobID:     st.JobID,
		Divergent: append([]string(nil), st.Verdict.Divergent...),
		NoReveal:  append([]string(nil), st.Verdict.NoReveal...),
	}
	j.emit(JobEvent{Kind: EventExpired, JobID: st.JobID})
	j.finish(res, ErrJobExpiredNoQuorum)
	return gen.TerminateReasonNormal
}

func (j *JobActor) verifyWinnerDigest(winnerCID string, acceptedHash []byte) error {
	verifyDir, err := os.MkdirTemp("", "vc-verify-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(verifyDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := j.cfg.Store.Fetch(ctx, winnerCID, verifyDir); err != nil {
		return fmt.Errorf("failed to fetch winner output CID %s: %w", winnerCID, err)
	}
	digest, err := canon.DirectoryDigest(verifyDir, canon.Limits{MaxBytes: 10 * 1024 * 1024, MaxFiles: 100})
	if err != nil {
		return fmt.Errorf("failed to compute directory digest for winner output: %w", err)
	}
	if !bytes.Equal(digest[:], acceptedHash) {
		return fmt.Errorf("winner output digest mismatch: got %x, want %x", digest[:], acceptedHash)
	}
	return nil
}

// Self-votes are skipped because Freechains either rejects or penalizes self-liking
// and a submitter does not pay reputation to itself.
func (j *JobActor) vote(like bool, amount int64, author string) {
	if author == "" || author == j.cfg.PublicKey {
		return
	}
	_, _ = j.Call(j.cfg.Bridge, VoteReq{
		Like:   like,
		Amount: amount,
		Author: author,
	})
}

func (j *JobActor) emit(ev JobEvent) {
	if j.cfg.OnEvent != nil {
		j.cfg.OnEvent(ev)
	}
}

func (j *JobActor) finish(res JobResult, err error) {
	if j.cfg.OnDone != nil {
		j.cfg.OnDone(res, err)
	}
}
