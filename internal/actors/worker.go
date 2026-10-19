package actors

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/internal/canon"
	"github.com/VictorMilhomem/freecompute/internal/protocol"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

const (
	BehaviorHonest    = "honest"
	BehaviorDivergent = "divergent"
)

type WorkerConfig struct {
	NodeID    string
	PublicKey string
	Behavior  string
	Bridge    gen.Atom
}

type jobMemo struct {
	executed   bool
	committed  bool
	revealed   bool
	nonce      []byte
	resultHash []byte
	resultCID  string
	workDir    string
	outputDir  string
}

type WorkerActor struct {
	act.Actor

	store  Store
	runner Runner
	cfg    WorkerConfig
	jobs   map[string]*jobMemo
}

func NewWorkerActor(store Store, runner Runner, cfg WorkerConfig) *WorkerActor {
	if cfg.Behavior == "" {
		cfg.Behavior = BehaviorHonest
	}
	return &WorkerActor{
		store:  store,
		runner: runner,
		cfg:    cfg,
		jobs:   make(map[string]*jobMemo),
	}
}

func (w *WorkerActor) Init(args ...any) error {
	return nil
}

func (w *WorkerActor) Terminate(reason error) {
	for _, m := range w.jobs {
		if m.workDir != "" {
			_ = os.RemoveAll(m.workDir)
		}
	}
}

func (w *WorkerActor) HandleMessage(from gen.PID, message any) error {
	upd, ok := message.(ChainUpdate)
	if !ok {
		return nil
	}

	st := protocol.Replay(upd.Entries)
	if st.Announce == nil {
		return nil
	}

	ctx := context.Background()
	memo := w.jobs[st.JobID]
	if memo == nil {
		memo = &jobMemo{}
		w.jobs[st.JobID] = memo
	}

	if !memo.committed && st.Phase == protocol.Committing && !w.hasCommitted(st) {
		if !memo.executed {
			if err := w.executeJob(ctx, st, memo); err != nil {
				w.Log().Error("worker %s execution failed: %v", w.cfg.NodeID, err)
				fmt.Fprintf(os.Stderr, "worker %s execution failed: %v\n", w.cfg.NodeID, err)
				return nil
			}
		}
		if err := w.publishCommit(st, memo); err != nil {
			w.Log().Error("worker %s commit failed: %v", w.cfg.NodeID, err)
			fmt.Fprintf(os.Stderr, "worker %s commit failed: %v\n", w.cfg.NodeID, err)
			return nil
		}
	}

	if memo.executed && !memo.revealed && st.Phase == protocol.Revealing && w.hasCommitted(st) && !w.hasRevealed(st) {
		if err := w.publishReveal(ctx, st, memo); err != nil {
			w.Log().Error("worker %s reveal failed: %v", w.cfg.NodeID, err)
			fmt.Fprintf(os.Stderr, "worker %s reveal failed: %v\n", w.cfg.NodeID, err)
			return nil
		}
	}

	if st.Phase == protocol.Done && memo.workDir != "" {
		_ = os.RemoveAll(memo.workDir)
		memo.workDir = ""
	}

	return nil
}

func (w *WorkerActor) hasCommitted(st protocol.State) bool {
	for _, c := range st.Commits {
		if c.Author == w.cfg.PublicKey {
			return true
		}
	}
	return false
}

func (w *WorkerActor) hasRevealed(st protocol.State) bool {
	for _, r := range st.Reveals {
		if r.Author == w.cfg.PublicKey {
			return true
		}
	}
	return false
}

func (w *WorkerActor) executeJob(ctx context.Context, st protocol.State, memo *jobMemo) error {
	workDir, err := os.MkdirTemp("", "vc-job-*")
	if err != nil {
		return err
	}
	if err := os.Chmod(workDir, 0755); err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}
	inputDir := filepath.Join(workDir, "input")
	outputDir := filepath.Join(workDir, "output")

	if err := os.MkdirAll(inputDir, 0755); err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}

	if err := os.MkdirAll(outputDir, 0777); err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}
	if err := os.Chmod(outputDir, 0777); err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}

	if err := w.store.Fetch(ctx, st.Announce.InputCid, inputDir); err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}

	if _, err := w.runner.Run(ctx, st.Announce.ImageRef, st.Announce.Cmd, inputDir, outputDir); err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}

	if w.cfg.Behavior == BehaviorDivergent {
		resPath := filepath.Join(outputDir, "result.txt")

		_ = os.Remove(resPath)
		if err := os.WriteFile(resPath, []byte("999\n"), 0644); err != nil {
			_ = os.RemoveAll(workDir)
			return err
		}
	}

	digest, err := canon.DirectoryDigest(outputDir, canon.Limits{
		MaxBytes: 10 * 1024 * 1024,
		MaxFiles: 100,
	})
	if err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}

	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		_ = os.RemoveAll(workDir)
		return err
	}

	memo.executed = true
	memo.nonce = nonce
	memo.resultHash = append([]byte(nil), digest[:]...)
	memo.workDir = workDir
	memo.outputDir = outputDir
	return nil
}

func (w *WorkerActor) publishCommit(st protocol.State, memo *jobMemo) error {
	commitment := protocol.Commitment(st.JobID, w.cfg.PublicKey, memo.resultHash, memo.nonce)
	env := &vcv1.Envelope{
		Version: 1,
		JobId:   st.JobID,
		Body: &vcv1.Envelope_Commit{
			Commit: &vcv1.ResultCommit{
				Commitment: commitment,
			},
		},
	}

	rawResp, err := w.CallWithTimeout(w.cfg.Bridge, PublishReq{Envelope: env}, 30)
	if err != nil {
		return err
	}
	resp, ok := rawResp.(PublishResp)
	if !ok || resp.Err != nil {
		return resp.Err
	}
	memo.committed = true
	return nil
}

func (w *WorkerActor) publishReveal(ctx context.Context, st protocol.State, memo *jobMemo) error {
	if memo.resultCID == "" {
		cid, err := w.store.AddDir(ctx, memo.outputDir)
		if err != nil {
			return err
		}
		memo.resultCID = cid
	}

	env := &vcv1.Envelope{
		Version: 1,
		JobId:   st.JobID,
		Body: &vcv1.Envelope_Reveal{
			Reveal: &vcv1.ResultReveal{
				ResultHash: memo.resultHash,
				ResultCid:  memo.resultCID,
				Nonce:      memo.nonce,
				ExitCode:   0,
			},
		},
	}

	rawResp, err := w.CallWithTimeout(w.cfg.Bridge, PublishReq{Envelope: env}, 30)
	if err != nil {
		return err
	}
	resp, ok := rawResp.(PublishResp)
	if !ok || resp.Err != nil {
		return resp.Err
	}

	memo.revealed = true
	if memo.workDir != "" {
		_ = os.RemoveAll(memo.workDir)
		memo.workDir = ""
	}
	return nil
}
