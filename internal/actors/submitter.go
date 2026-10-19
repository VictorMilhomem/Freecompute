package actors

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/internal/canon"
	"github.com/VictorMilhomem/freecompute/internal/protocol"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

type SubmitterConfig struct {
	NodeID      string
	InputFile   string
	ImageRef    string
	Cmd         []string
	Replicas    uint32
	Quorum      uint32
	Reward      uint64
	GracePeriod time.Duration
	Bridge      gen.Atom
	ReportOut   io.Writer
	Done        chan struct{}
}

type SubmitterReport struct {
	JobID      string
	Verdict    protocol.Verdict
	Verified   bool
	ResultText string
	Err        error
}

type startSubmit struct{}
type graceTick struct{}

type SubmitterActor struct {
	act.Actor

	store          Store
	cfg            SubmitterConfig
	announced      bool
	revealTimerSet bool
	graceExpired   bool
	finalized      bool
	lastState      protocol.State

	mu     sync.Mutex
	report SubmitterReport
}

func NewSubmitterActor(store Store, cfg SubmitterConfig) *SubmitterActor {
	if cfg.Replicas == 0 {
		cfg.Replicas = 3
	}
	if cfg.Quorum == 0 {
		cfg.Quorum = 2
	}
	if cfg.Reward == 0 {
		cfg.Reward = 1000
	}
	if cfg.GracePeriod <= 0 {
		cfg.GracePeriod = 20 * time.Second
	}
	if len(cfg.Cmd) == 0 {
		cfg.Cmd = []string{"sh", "-c", "wc -w < /input/data.txt > /output/result.txt"}
	}
	if cfg.ReportOut == nil {
		cfg.ReportOut = os.Stdout
	}
	return &SubmitterActor{
		store: store,
		cfg:   cfg,
	}
}

func (s *SubmitterActor) Report() SubmitterReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.report
}

func (s *SubmitterActor) Init(args ...any) error {
	_ = s.Send(s.PID(), startSubmit{})
	return nil
}

func (s *SubmitterActor) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case startSubmit:
		if err := s.announceJob(context.Background()); err != nil {
			s.finishWithError(err)
		}

	case ChainUpdate:
		st := protocol.Replay(msg.Entries)
		s.lastState = st
		if st.Announce == nil {
			return nil
		}
		if st.Phase == protocol.Revealing && !s.revealTimerSet {
			s.revealTimerSet = true

			_, _ = s.SendAfter(s.PID(), graceTick{}, s.cfg.GracePeriod)
		}
		s.maybeFinalize(context.Background())

	case graceTick:
		s.graceExpired = true
		s.maybeFinalize(context.Background())
	}
	return nil
}

func (s *SubmitterActor) announceJob(ctx context.Context) error {
	if s.announced {
		return nil
	}
	f, err := os.Open(s.cfg.InputFile)
	if err != nil {
		return fmt.Errorf("failed to open input file %s: %w", s.cfg.InputFile, err)
	}
	defer f.Close()

	inputCID, err := s.store.Add(ctx, f)
	if err != nil {
		return fmt.Errorf("failed to add input to ipfs: %w", err)
	}

	ann := &vcv1.JobAnnounce{
		ImageRef:   s.cfg.ImageRef,
		Cmd:        s.cfg.Cmd,
		InputCid:   inputCID,
		Replicas:   s.cfg.Replicas,
		Quorum:     s.cfg.Quorum,
		RewardReps: s.cfg.Reward,
	}
	jobID, err := protocol.JobID(ann)
	if err != nil {
		return err
	}

	env := &vcv1.Envelope{
		Version: 1,
		JobId:   jobID,
		Body: &vcv1.Envelope_Announce{
			Announce: ann,
		},
	}

	rawResp, err := s.CallWithTimeout(s.cfg.Bridge, PublishReq{Envelope: env}, 30)
	if err != nil {
		return err
	}
	resp, ok := rawResp.(PublishResp)
	if !ok || resp.Err != nil {
		return resp.Err
	}

	s.announced = true
	return nil
}

func (s *SubmitterActor) maybeFinalize(ctx context.Context) {
	if s.finalized || s.lastState.Phase != protocol.Revealing {
		return
	}

	st := s.lastState
	shouldFinalize := st.AllRevealed || (s.graceExpired && st.Verdict.HasQuorum)
	if !shouldFinalize {
		return
	}

	s.finalized = true

	var verified bool
	var resultText string
	if st.Verdict.HasQuorum && st.Verdict.WinnerCID != "" {
		verifyDir, err := os.MkdirTemp("", "vc-verify-*")
		if err != nil {
			s.finishWithError(err)
			return
		}
		defer os.RemoveAll(verifyDir)

		if err := s.store.Fetch(ctx, st.Verdict.WinnerCID, verifyDir); err != nil {
			s.finishWithError(fmt.Errorf("failed to fetch winner result %s: %w", st.Verdict.WinnerCID, err))
			return
		}

		digest, err := canon.DirectoryDigest(verifyDir, canon.Limits{
			MaxBytes: 10 * 1024 * 1024,
			MaxFiles: 100,
		})
		if err != nil {
			s.finishWithError(fmt.Errorf("failed to hash winner directory: %w", err))
			return
		}
		if !bytes.Equal(digest[:], st.Verdict.AcceptedHash) {
			s.finishWithError(fmt.Errorf("winner result digest mismatch: got %x, want %x", digest[:], st.Verdict.AcceptedHash))
			return
		}
		verified = true
		if b, err := os.ReadFile(filepath.Join(verifyDir, "result.txt")); err == nil {
			resultText = strings.TrimSpace(string(b))
		}
	}

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

	rawResp, err := s.CallWithTimeout(s.cfg.Bridge, PublishReq{Envelope: finEnv}, 30)
	if err != nil {
		s.finishWithError(err)
		return
	}
	if resp, ok := rawResp.(PublishResp); !ok || resp.Err != nil {
		s.finishWithError(resp.Err)
		return
	}

	reward := int64(st.Announce.RewardReps)
	for _, w := range st.Verdict.Winners {
		_, _ = s.CallWithTimeout(s.cfg.Bridge, VoteReq{Like: true, Amount: reward, Author: w}, 30)
	}
	for _, d := range st.Verdict.Divergent {
		_, _ = s.CallWithTimeout(s.cfg.Bridge, VoteReq{Like: false, Amount: reward, Author: d}, 30)
	}
	for _, nr := range st.Verdict.NoReveal {
		_, _ = s.CallWithTimeout(s.cfg.Bridge, VoteReq{Like: false, Amount: reward, Author: nr}, 30)
	}

	fmt.Fprintf(s.cfg.ReportOut, "JOB_ID=%s\n", st.JobID)
	fmt.Fprintf(s.cfg.ReportOut, "ACCEPTED_HASH=%s\n", hex.EncodeToString(st.Verdict.AcceptedHash))
	fmt.Fprintf(s.cfg.ReportOut, "VERIFIED=%v\n", verified)
	fmt.Fprintf(s.cfg.ReportOut, "RESULT=%s\n", resultText)
	fmt.Fprintf(s.cfg.ReportOut, "WINNERS=%s\n", strings.Join(st.Verdict.Winners, ","))
	fmt.Fprintf(s.cfg.ReportOut, "DIVERGENT=%s\n", strings.Join(st.Verdict.Divergent, ","))
	fmt.Fprintf(s.cfg.ReportOut, "NO_REVEAL=%s\n", strings.Join(st.Verdict.NoReveal, ","))

	s.mu.Lock()
	s.report = SubmitterReport{
		JobID:      st.JobID,
		Verdict:    st.Verdict,
		Verified:   verified,
		ResultText: resultText,
	}
	s.mu.Unlock()

	if s.cfg.Done != nil {
		close(s.cfg.Done)
	}
}

func (s *SubmitterActor) finishWithError(err error) {
	s.Log().Error("submitter failed: %v", err)
	s.mu.Lock()
	s.report.Err = err
	s.mu.Unlock()
	if s.cfg.Done != nil {
		close(s.cfg.Done)
	}
}
