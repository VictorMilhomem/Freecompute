package actors

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/internal/canon"
	"github.com/VictorMilhomem/freecompute/internal/protocol"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

type SimulateCrashMsg struct{}

type delayedFinalizeMsg struct{}

type TaskActorConfig struct {
	NodeID            string
	PublicKey         string
	JobID             string
	StateDir          string
	TickAfter         time.Duration
	HasLocalSubmitter bool
	Dispatcher        gen.Atom
	Bridge            gen.Atom
	Store             Store
	Runner            Runner
	RunCtx            context.Context
	Logger            *slog.Logger
}

type TaskActor struct {
	act.Actor
	cfg               TaskActorConfig
	state             protocol.State
	persisted         *PersistedTaskState
	slotRequested     bool
	slotGranted       bool
	committed         bool
	revealed          bool
	revealSkipped     bool
	finalizeScheduled bool
	finalizePosted    bool
}

func NewTaskActor(cfg TaskActorConfig) *TaskActor {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.RunCtx == nil {
		cfg.RunCtx = context.Background()
	}
	return &TaskActor{
		cfg: cfg,
	}
}

func (t *TaskActor) Init(args ...any) error {
	st, err := LoadTaskState(t.cfg.StateDir, t.cfg.JobID)
	if err == nil && st != nil {
		t.persisted = st
	}
	return nil
}

func (t *TaskActor) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case SimulateCrashMsg:
		return errors.New("simulated task actor crash")

	case SlotGrantedMsg:
		if msg.JobID != t.cfg.JobID {
			return nil
		}
		t.slotGranted = true
		if t.state.Announce != nil && t.state.Phase == protocol.Committing && !t.committed {
			t.handleCommitting()
		}

	case delayedFinalizeMsg:
		if t.state.Finalizable && t.state.Finalize == nil && !t.finalizePosted {
			t.postFinalize()
		}

	case JobStateUpdate:
		return t.onStateUpdate(msg.State)
	}
	return nil
}

func (t *TaskActor) onStateUpdate(st protocol.State) error {
	t.state = st
	if st.Announce == nil {
		return nil
	}

	if st.Finalize != nil {
		t.releaseSlot()
		if t.persisted != nil && t.persisted.OutputDir != "" {
			_ = os.RemoveAll(filepath.Dir(t.persisted.OutputDir))
		}
		_ = RemoveTaskState(t.cfg.StateDir, t.cfg.JobID)
		return gen.TerminateReasonNormal
	}

	for _, c := range st.Commits {
		if c.Author == t.cfg.PublicKey {
			t.committed = true
			break
		}
	}
	for _, r := range st.Reveals {
		if r.Author == t.cfg.PublicKey {
			t.revealed = true
			break
		}
	}

	if st.Phase == protocol.Committing && !t.committed {
		t.handleCommitting()
	}

	if st.Phase == protocol.Revealing {
		t.releaseSlot()
		if !t.revealed && !t.revealSkipped && !st.Finalizable && t.isCounted(st) {
			t.handleRevealing(st)
		}
	}

	if st.Finalizable && st.Finalize == nil && !t.cfg.HasLocalSubmitter && !t.finalizeScheduled && !t.finalizePosted {
		t.finalizeScheduled = true
		delay := 5 * time.Second
		if t.cfg.TickAfter > 0 && t.cfg.TickAfter < time.Second {
			delay = 100 * time.Millisecond
		}
		_, _ = t.SendAfter(t.PID(), delayedFinalizeMsg{}, delay)
	}

	return nil
}

func (t *TaskActor) handleCommitting() {
	if t.committed || t.cfg.RunCtx.Err() != nil {
		return
	}

	if t.persisted == nil {
		if loaded, err := LoadTaskState(t.cfg.StateDir, t.cfg.JobID); err == nil && loaded != nil {
			t.persisted = loaded
		}
	}

	if t.persisted != nil {
		t.publishCommit()
		return
	}

	if !t.slotGranted {
		if !t.slotRequested {
			t.slotRequested = true
			_ = t.Send(t.cfg.Dispatcher, AcquireSlotReq{
				JobID: t.cfg.JobID,
				Task:  t.Name(),
			})
		}
		return
	}

	defer t.releaseSlot()

	outputDir, exitCode, digest, err := t.runContainer(t.state.Announce)
	if err != nil {
		return
	}

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return
	}

	persisted := PersistedTaskState{
		JobID:      t.cfg.JobID,
		Nonce:      nonce,
		ResultHash: append([]byte(nil), digest[:]...),
		OutputDir:  outputDir,
		ExitCode:   exitCode,
	}
	if err := SaveTaskState(t.cfg.StateDir, persisted); err != nil {
		t.cfg.Logger.Error("failed to persist worker state before commit", "job_id", t.cfg.JobID, "err", err)
		return
	}
	t.persisted = &persisted
	t.publishCommit()
}

func (t *TaskActor) publishCommit() {
	if t.persisted == nil || t.committed {
		return
	}
	commitment := protocol.Commitment(t.cfg.JobID, t.cfg.PublicKey, t.persisted.ResultHash, t.persisted.Nonce)
	env := &vcv1.Envelope{
		Version: 1,
		JobId:   t.cfg.JobID,
		Body: &vcv1.Envelope_Commit{
			Commit: &vcv1.ResultCommit{
				Commitment: commitment,
			},
		},
	}
	respAny, err := t.Call(t.cfg.Bridge, PublishReq{Envelope: env})
	if err != nil {
		return
	}
	if resp, ok := respAny.(PublishResp); ok && resp.Err != nil {
		return
	}
	t.committed = true
}

func (t *TaskActor) handleRevealing(st protocol.State) {
	if t.cfg.RunCtx.Err() != nil {
		return
	}
	if t.persisted == nil {
		loaded, err := LoadTaskState(t.cfg.StateDir, t.cfg.JobID)
		if err != nil || loaded == nil {
			t.cfg.Logger.Error("missing persisted state at reveal time; skipping reveal", "job_id", t.cfg.JobID)
			t.revealSkipped = true
			return
		}
		t.persisted = loaded
	}

	if !dirExists(t.persisted.OutputDir) {
		newOutputDir, newExitCode, newDigest, err := t.runContainer(st.Announce)
		if err != nil {
			t.cfg.Logger.Error("failed to re-run container for missing output dir", "job_id", t.cfg.JobID, "err", err)
			t.revealSkipped = true
			return
		}
		if !bytes.Equal(newDigest[:], t.persisted.ResultHash) {
			t.cfg.Logger.Error("re-run output digest mismatch; skipping reveal", "job_id", t.cfg.JobID)
			_ = os.RemoveAll(filepath.Dir(newOutputDir))
			t.revealSkipped = true
			return
		}
		t.persisted.OutputDir = newOutputDir
		t.persisted.ExitCode = newExitCode
		_ = SaveTaskState(t.cfg.StateDir, *t.persisted)
	}

	ctx, cancel := context.WithTimeout(t.cfg.RunCtx, 30*time.Second)
	defer cancel()

	resultCID, err := t.cfg.Store.AddDir(ctx, t.persisted.OutputDir)
	if err != nil {
		return
	}

	env := &vcv1.Envelope{
		Version: 1,
		JobId:   t.cfg.JobID,
		Body: &vcv1.Envelope_Reveal{
			Reveal: &vcv1.ResultReveal{
				ResultHash: t.persisted.ResultHash,
				ResultCid:  resultCID,
				Nonce:      t.persisted.Nonce,
				ExitCode:   t.persisted.ExitCode,
			},
		},
	}
	respAny, err := t.Call(t.cfg.Bridge, PublishReq{Envelope: env})
	if err != nil {
		return
	}
	if resp, ok := respAny.(PublishResp); ok && resp.Err != nil {
		return
	}
	t.revealed = true
}

func (t *TaskActor) postFinalize() {
	if !t.state.Finalizable || t.state.Finalize != nil || t.finalizePosted {
		return
	}
	t.finalizePosted = true
	env := &vcv1.Envelope{
		Version: 1,
		JobId:   t.cfg.JobID,
		Body: &vcv1.Envelope_Finalize{
			Finalize: &vcv1.JobFinalize{
				AcceptedHash: t.state.Verdict.AcceptedHash,
				Winners:      t.state.Verdict.Winners,
				Divergent:    t.state.Verdict.Divergent,
				NoReveal:     t.state.Verdict.NoReveal,
			},
		},
	}
	_, _ = t.Call(t.cfg.Bridge, PublishReq{Envelope: env})
}

func (t *TaskActor) runContainer(ann *vcv1.JobAnnounce) (string, int32, [32]byte, error) {
	var zero [32]byte
	if t.cfg.RunCtx.Err() != nil {
		return "", -1, zero, t.cfg.RunCtx.Err()
	}

	workDir, err := os.MkdirTemp("", fmt.Sprintf("vc-worker-%s-*", t.cfg.NodeID))
	if err != nil {
		return "", -1, zero, err
	}
	if err := os.Chmod(workDir, 0o755); err != nil {
		_ = os.RemoveAll(workDir)
		return "", -1, zero, err
	}

	inputDir := filepath.Join(workDir, "input")
	outputDir := filepath.Join(workDir, "output")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		_ = os.RemoveAll(workDir)
		return "", -1, zero, err
	}
	// Rootless Podman maps container UID 65534 (nobody) to a host subuid; mode 0777
	// allows the container process to write into the bind-mounted output directory.
	if err := os.MkdirAll(outputDir, 0o777); err != nil {
		_ = os.RemoveAll(workDir)
		return "", -1, zero, err
	}
	if err := os.Chmod(outputDir, 0o777); err != nil {
		_ = os.RemoveAll(workDir)
		return "", -1, zero, err
	}

	ctx, cancel := context.WithTimeout(t.cfg.RunCtx, 60*time.Second)
	defer cancel()

	if err := t.cfg.Store.Fetch(ctx, ann.InputCid, inputDir); err != nil {
		_ = os.RemoveAll(workDir)
		return "", -1, zero, err
	}

	exitCode, err := t.cfg.Runner.Run(ctx, ann.ImageRef, ann.Cmd, inputDir, outputDir)
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", -1, zero, err
	}

	digest, err := canon.DirectoryDigest(outputDir, canon.Limits{MaxBytes: 10 * 1024 * 1024, MaxFiles: 100})
	if err != nil {
		_ = os.RemoveAll(workDir)
		return "", -1, zero, err
	}
	return outputDir, exitCode, digest, nil
}

func (t *TaskActor) isCounted(st protocol.State) bool {
	for _, c := range st.Commits {
		if c.Author == t.cfg.PublicKey {
			return true
		}
	}
	return false
}

func (t *TaskActor) releaseSlot() {
	if t.slotRequested || t.slotGranted {
		t.slotRequested = false
		t.slotGranted = false
		_ = t.Send(t.cfg.Dispatcher, ReleaseSlotMsg{JobID: t.cfg.JobID})
	}
}

func dirExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
