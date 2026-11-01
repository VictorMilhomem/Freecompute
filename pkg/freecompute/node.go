package freecompute

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/freechains"
	"github.com/VictorMilhomem/freecompute/internal/actors"
	"github.com/VictorMilhomem/freecompute/internal/ipfs"
	"github.com/VictorMilhomem/freecompute/internal/sandbox"
)

type Config struct {
	ID           string
	Root, Key    string
	Chain        string
	Peers        []string
	StateDir     string
	PollInterval time.Duration
	TickAfter    time.Duration
}

type Node struct {
	mu            sync.Mutex
	cfg           Config
	pubKey        string
	postCost      int64
	maxTicks      int
	store         Store
	sbRunner      sandbox.Runner
	runner        actors.Runner
	chain         ChainAPI
	logger        *slog.Logger
	ergoNode      gen.Node
	supAtom       gen.Atom
	dispAtom      gen.Atom
	bridgeAtom    gen.Atom
	runCtx        context.Context
	runCancel     context.CancelFunc
	pendingPolicy *actors.WorkerPolicyConfig
	jobs          []*Job
	seq           uint64
	started       bool
	stopped       bool
}

func NewNode(cfg Config, opts ...Option) (*Node, error) {
	if cfg.ID == "" {
		return nil, fmt.Errorf("%w: ID is required", ErrInvalidSpec)
	}
	if cfg.Chain == "" {
		cfg.Chain = "#vc"
	}
	if cfg.StateDir == "" && cfg.Root != "" {
		cfg.StateDir = filepath.Join(cfg.Root, "vc-state")
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	if cfg.TickAfter <= 0 {
		cfg.TickAfter = 10 * time.Second
	}

	n := &Node{
		cfg:        cfg,
		postCost:   DefaultPostCost,
		maxTicks:   DefaultMaxTicks,
		logger:     slog.Default(),
		supAtom:    gen.Atom("vc_supervisor"),
		dispAtom:   gen.Atom("dispatcher"),
		bridgeAtom: gen.Atom("chain_bridge"),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(n)
		}
	}

	if n.chain == nil && (cfg.Root == "" || cfg.Key == "") {
		return nil, fmt.Errorf("%w: Root and Key are required when ChainAPI is not provided", ErrInvalidSpec)
	}
	return n, nil
}

func (n *Node) Start(ctx context.Context) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.stopped {
		return ErrNodeStopped
	}
	if n.started {
		return nil
	}

	if n.pubKey == "" {
		if n.cfg.Key != "" {
			pub, err := actors.ReadPublicKey(n.cfg.Key)
			if err != nil {
				return err
			}
			n.pubKey = pub
		} else {
			n.pubKey = n.cfg.ID
		}
	}

	if n.chain == nil {
		fcClient, err := freechains.NewClient(
			freechains.NewSerialRunner(freechains.NewExecRunner()),
			freechains.Config{
				Root:           n.cfg.Root,
				DefaultKey:     freechains.PrivateKeyPath(n.cfg.Key),
				CommandTimeout: 30 * time.Second,
			},
		)
		if err != nil {
			return fmt.Errorf("freechains client init failed: %w", err)
		}
		n.chain = actors.NewFreechainsAdapter(fcClient, n.cfg.Chain, n.cfg.Key)
	}

	if n.store == nil {
		ipfsStore, err := ipfs.NewStore(ipfs.Config{
			APIAddr:        os.Getenv("VC_IPFS_API"),
			RequestTimeout: 30 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("ipfs store init failed: %w", err)
		}
		n.store = actors.NewIPFSAdapter(ipfsStore, ipfs.FetchLimits{
			MaxBytes: 10 * 1024 * 1024,
			MaxFiles: 100,
			MaxDepth: 8,
		})
	}

	defaultLimits := sandbox.Limits{
		CPUs:           1.0,
		MemoryBytes:    256 * 1024 * 1024,
		Timeout:        60 * time.Second,
		PidsLimit:      100,
		TmpfsBytes:     64 * 1024 * 1024,
		MaxOutputBytes: 10 * 1024 * 1024,
		MaxOutputFiles: 100,
	}
	if n.sbRunner == nil {
		sbRunner, err := sandbox.NewRunner(sandbox.Config{
			SocketURI:     os.Getenv("VC_PODMAN_SOCKET"),
			MaxConcurrent: 1,
			DefaultLimits: defaultLimits,
		})
		if err != nil {
			return fmt.Errorf("podman runner init failed: %w", err)
		}
		n.sbRunner = sbRunner
	}
	if n.runner == nil {
		n.runner = actors.NewSandboxAdapter(n.sbRunner, defaultLimits)
	}

	if err := n.store.Ping(ctx); err != nil {
		return fmt.Errorf("IPFS daemon unreachable: %w", err)
	}
	if err := n.runner.Ping(ctx); err != nil {
		return fmt.Errorf("Podman service unreachable: %w", err)
	}
	if err := n.runner.Cleanup(ctx); err != nil {
		return fmt.Errorf("Podman startup cleanup failed: %w", err)
	}

	n.runCtx, n.runCancel = context.WithCancel(context.Background())

	nodeOpts := gen.NodeOptions{}
	nodeOpts.Network.Mode = gen.NetworkModeDisabled
	nodeOpts.Log.Level = gen.LogLevelError
	nodeOpts.Log.DefaultLogger.Disable = true

	seq := atomic.AddUint64(&n.seq, 1)
	nodeName := gen.Atom(fmt.Sprintf("%s_%d_%d@localhost", n.cfg.ID, time.Now().UnixNano(), seq))
	en, err := ergo.StartNode(nodeName, nodeOpts)
	if err != nil {
		n.runCancel()
		return fmt.Errorf("failed to start ergo node: %w", err)
	}
	n.ergoNode = en

	dCfg := actors.DispatcherConfig{
		NodeID:     n.cfg.ID,
		PublicKey:  n.pubKey,
		StateDir:   n.cfg.StateDir,
		TickAfter:  n.cfg.TickAfter,
		MaxTicks:   n.maxTicks,
		Supervisor: n.supAtom,
		Bridge:     n.bridgeAtom,
		Store:      n.store,
		Runner:     n.runner,
		RunCtx:     n.runCtx,
		Logger:     n.logger,
	}
	bCfg := actors.BridgeConfig{
		Peers:        append([]string(nil), n.cfg.Peers...),
		PollInterval: n.cfg.PollInterval,
		Targets:      []gen.Atom{n.dispAtom},
	}

	if _, err := en.SpawnRegister(n.supAtom, func() gen.ProcessBehavior {
		return actors.NewNodeSupervisor(n.chain, n.bridgeAtom, bCfg, n.dispAtom, dCfg)
	}, gen.ProcessOptions{}); err != nil {
		en.StopForce()
		n.runCancel()
		return fmt.Errorf("failed to spawn node supervisor: %w", err)
	}

	n.started = true

	if n.pendingPolicy != nil {
		if err := n.enableWorkerLocked(*n.pendingPolicy); err != nil {
			en.StopForce()
			n.runCancel()
			n.started = false
			return err
		}
	}
	return nil
}

func (n *Node) Stop(ctx context.Context) error {
	n.mu.Lock()
	if n.stopped {
		n.mu.Unlock()
		return nil
	}
	n.stopped = true
	started := n.started
	en := n.ergoNode
	cancel := n.runCancel
	runner := n.runner
	jobs := append([]*Job(nil), n.jobs...)
	n.mu.Unlock()

	if started && en != nil {
		done := make(chan struct{})
		if err := en.Send(n.dispAtom, actors.StopDispatcherMsg{Reply: done}); err == nil {
			select {
			case <-done:
			case <-time.After(100 * time.Millisecond):
			}
		}
	}

	if cancel != nil {
		cancel()
	}
	if runner != nil {
		_ = runner.Cleanup(ctx)
	}
	for _, j := range jobs {
		j.resolve(Result{JobID: j.ID()}, ErrNodeStopped)
	}
	if started && en != nil {
		en.Stop()
	}
	return nil
}

func (n *Node) Fetch(ctx context.Context, cid string, dest string) error {
	n.mu.Lock()
	if n.stopped {
		n.mu.Unlock()
		return ErrNodeStopped
	}
	store := n.store
	n.mu.Unlock()

	if store == nil {
		return fmt.Errorf("%w: store not initialized", ErrInvalidSpec)
	}
	return store.Fetch(ctx, cid, dest)
}

func (n *Node) KillTaskActorForTest(jobID string) error {
	n.mu.Lock()
	en := n.ergoNode
	n.mu.Unlock()
	if en == nil {
		return ErrNodeStopped
	}
	return en.Send(gen.Atom("task_"+jobID), actors.SimulateCrashMsg{})
}
