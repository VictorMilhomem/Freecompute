package freecompute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/VictorMilhomem/freecompute/internal/actors"
	"github.com/VictorMilhomem/freecompute/internal/protocol"
	"github.com/VictorMilhomem/freecompute/internal/sandbox"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

const eventBufferCapacity = 64

type Job struct {
	id      string
	events  chan Event
	evMu    sync.Mutex
	dropped atomic.Uint64

	doneOnce sync.Once
	doneCh   chan struct{}
	res      Result
	err      error
}

func newJob(id string) *Job {
	return &Job{
		id:     id,
		events: make(chan Event, eventBufferCapacity),
		doneCh: make(chan struct{}),
	}
}

func (j *Job) ID() string {
	return j.id
}

func (j *Job) Events() <-chan Event {
	return j.events
}

func (j *Job) DroppedEvents() uint64 {
	return j.dropped.Load()
}

func (j *Job) Wait(ctx context.Context) (Result, error) {
	select {
	case <-j.doneCh:
		return j.res, j.err
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

// Push never blocks the actor loop; if the bounded channel is full, the oldest
// buffered event is discarded and counted.
func (j *Job) pushEvent(ev Event) {
	j.evMu.Lock()
	defer j.evMu.Unlock()

	select {
	case j.events <- ev:
		return
	default:
		select {
		case <-j.events:
			j.dropped.Add(1)
		default:
		}
		select {
		case j.events <- ev:
		default:
		}
	}
}

func (j *Job) resolve(res Result, err error) {
	j.doneOnce.Do(func() {
		j.res = res
		j.err = err
		close(j.doneCh)
	})
}

func (n *Node) Submit(ctx context.Context, spec JobSpec) (*Job, error) {
	n.mu.Lock()
	if n.stopped {
		n.mu.Unlock()
		return nil, ErrNodeStopped
	}
	if !n.started {
		n.mu.Unlock()
		return nil, fmt.Errorf("%w: node not started", ErrInvalidSpec)
	}
	n.mu.Unlock()

	if spec.CommitWindow == 0 {
		spec.CommitWindow = DefaultCommitWindow
	}
	if spec.RevealWindow == 0 {
		spec.RevealWindow = DefaultRevealWindow
	}
	if _, err := sandbox.ParseImageRef(spec.Image); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}
	if len(spec.Cmd) == 0 {
		return nil, fmt.Errorf("%w: command must not be empty", ErrInvalidSpec)
	}
	if spec.Replicas < 1 || spec.Quorum < 1 || spec.Quorum > spec.Replicas {
		return nil, fmt.Errorf("%w: invalid replicas=%d quorum=%d", ErrInvalidSpec, spec.Replicas, spec.Quorum)
	}
	if spec.Reward < 0 {
		return nil, fmt.Errorf("%w: reward cannot be negative", ErrInvalidSpec)
	}
	if spec.CommitWindow == 0 || spec.RevealWindow == 0 {
		return nil, fmt.Errorf("%w: windows must be > 0", ErrInvalidSpec)
	}
	if spec.Input.kind == inputUnset {
		return nil, fmt.Errorf("%w: input is required", ErrInvalidSpec)
	}

	requiredReps := spec.Reward*int64(spec.Replicas) + 2*n.postCost
	repsReply := make(chan actors.RepsResp, 1)
	if err := n.ergoNode.Send(n.bridgeAtom, actors.RepsDirectMsg{
		Author: n.pubKey,
		Reply:  repsReply,
	}); err != nil {
		return nil, err
	}
	select {
	case rr := <-repsReply:
		if rr.Err != nil {
			return nil, rr.Err
		}
		if rr.Reps < requiredReps {
			return nil, fmt.Errorf("%w: have %d reps, need %d", ErrInsufficientReps, rr.Reps, requiredReps)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	inputCID, err := n.prepareInputDirCID(ctx, spec.Input)
	if err != nil {
		return nil, err
	}

	ann := &vcv1.JobAnnounce{
		ImageRef:     spec.Image,
		Cmd:          append([]string(nil), spec.Cmd...),
		InputCid:     inputCID,
		Replicas:     spec.Replicas,
		Quorum:       spec.Quorum,
		CommitWindow: spec.CommitWindow,
		RevealWindow: spec.RevealWindow,
		RewardReps:   uint64(spec.Reward),
	}
	jobID, err := protocol.JobID(ann)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}

	job := newJob(jobID)
	n.mu.Lock()
	if n.stopped {
		n.mu.Unlock()
		return nil, ErrNodeStopped
	}
	n.jobs = append(n.jobs, job)
	n.mu.Unlock()

	reply := make(chan error, 1)
	req := actors.SubmitJobReq{
		JobID:    jobID,
		Announce: ann,
		OnEvent: func(ev actors.JobEvent) {
			switch ev.Kind {
			case actors.EventAnnounced:
				job.pushEvent(Announced{JobID: ev.JobID})
			case actors.EventCommitted:
				job.pushEvent(Committed{JobID: ev.JobID, Worker: ev.Worker})
			case actors.EventRevealPhaseOpened:
				job.pushEvent(RevealPhaseOpened{JobID: ev.JobID})
			case actors.EventRevealed:
				job.pushEvent(Revealed{JobID: ev.JobID, Worker: ev.Worker})
			case actors.EventFinalized:
				job.pushEvent(Finalized{Result: toPublicResult(ev.Result)})
			case actors.EventExpired:
				job.pushEvent(Expired{JobID: ev.JobID})
			}
		},
		OnDone: func(res actors.JobResult, err error) {
			if err != nil {
				if errors.Is(err, actors.ErrJobExpiredNoQuorum) {
					err = fmt.Errorf("%w: %w", ErrJobExpired, ErrNoQuorum)
				}
				job.resolve(toPublicResult(res), err)
				return
			}
			job.resolve(toPublicResult(res), nil)
		},
		Reply: reply,
	}

	if err := n.ergoNode.Send(n.dispAtom, req); err != nil {
		return nil, err
	}
	select {
	case err := <-reply:
		if err != nil {
			return nil, err
		}
		return job, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Wrapping single files into a temporary directory before AddDir ensures the
// worker container always mounts a directory tree at /input with the original filename.
func (n *Node) prepareInputDirCID(ctx context.Context, in Input) (string, error) {
	switch in.kind {
	case inputCID:
		if in.cid == "" {
			return "", fmt.Errorf("%w: empty input CID", ErrInvalidSpec)
		}
		return in.cid, nil

	case inputDir:
		info, err := os.Stat(in.path)
		if err != nil || !info.IsDir() {
			return "", fmt.Errorf("%w: input directory %q invalid", ErrInvalidSpec, in.path)
		}
		return n.store.AddDir(ctx, in.path)

	case inputFile:
		data, err := os.ReadFile(in.path)
		if err != nil {
			return "", fmt.Errorf("%w: cannot read input file %q: %v", ErrInvalidSpec, in.path, err)
		}
		base := filepath.Base(in.path)
		return n.wrapBytesAsDir(ctx, base, data)

	case inputBytes:
		base := filepath.Base(in.name)
		if base == "" || base == "." || base == ".." {
			return "", fmt.Errorf("%w: invalid input filename %q", ErrInvalidSpec, in.name)
		}
		return n.wrapBytesAsDir(ctx, base, in.data)

	default:
		return "", fmt.Errorf("%w: unsupported input kind", ErrInvalidSpec)
	}
}

func (n *Node) wrapBytesAsDir(ctx context.Context, name string, data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "vc-input-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDir)

	dst := filepath.Join(tmpDir, name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return n.store.AddDir(ctx, tmpDir)
}

func toPublicResult(r actors.JobResult) Result {
	return Result{
		JobID:        r.JobID,
		AcceptedHash: r.AcceptedHash,
		OutputCID:    r.OutputCID,
		Winners:      append([]string(nil), r.Winners...),
		Divergent:    append([]string(nil), r.Divergent...),
		NoReveal:     append([]string(nil), r.NoReveal...),
	}
}
