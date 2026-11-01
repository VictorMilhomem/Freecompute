package freecompute

import (
	"fmt"

	"github.com/VictorMilhomem/freecompute/internal/actors"
)

type WorkerPolicy struct {
	AllowImages   []string
	AllowAnyImage bool
	AllowOwnJobs  bool
	MaxConcurrent int
}

func (n *Node) Serve(policy WorkerPolicy) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.stopped {
		return ErrNodeStopped
	}
	if policy.MaxConcurrent <= 0 {
		policy.MaxConcurrent = 1
	}

	imgPolicy, err := actors.NewAllowlistPolicy(policy.AllowImages, policy.AllowAnyImage)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSpec, err)
	}

	cfg := actors.WorkerPolicyConfig{
		AllowImages:   append([]string(nil), policy.AllowImages...),
		AllowAnyImage: policy.AllowAnyImage,
		AllowOwnJobs:  policy.AllowOwnJobs,
		MaxConcurrent: policy.MaxConcurrent,
		ImagePolicy:   imgPolicy,
	}

	n.pendingPolicy = &cfg
	if !n.started {
		return nil
	}
	return n.enableWorkerLocked(cfg)
}

func (n *Node) enableWorkerLocked(cfg actors.WorkerPolicyConfig) error {
	reply := make(chan error, 1)
	if err := n.ergoNode.Send(n.dispAtom, actors.EnableWorkerReq{
		Policy: cfg,
		Reply:  reply,
	}); err != nil {
		return err
	}
	return <-reply
}
