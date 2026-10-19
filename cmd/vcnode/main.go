package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ergo.services/ergo"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/freechains"
	"github.com/VictorMilhomem/freecompute/internal/actors"
	"github.com/VictorMilhomem/freecompute/internal/ipfs"
	"github.com/VictorMilhomem/freecompute/internal/sandbox"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "vcnode error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}

	fs := flag.NewFlagSet("vcnode", flag.ContinueOnError)
	id := fs.String("id", "", "node alias (e.g. alice, bob, carol)")
	root := fs.String("root", "", "freechains root directory")
	key := fs.String("key", "", "private key path")
	chain := fs.String("chain", "#vc", "freechains chain alias")
	peersCSV := fs.String("peers", "", "comma-separated peer host:port list")
	behavior := fs.String("behavior", actors.BehaviorHonest, "worker behavior: honest|divergent")
	submit := fs.String("submit", "", "input file path to submit as a job")
	grace := fs.Int("grace", 20, "submitter local grace timeout in seconds")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *id == "" {
		return fmt.Errorf("--id is required")
	}
	if *root == "" {
		return fmt.Errorf("--root is required")
	}
	if *key == "" {
		return fmt.Errorf("--key is required")
	}
	if *behavior != actors.BehaviorHonest && *behavior != actors.BehaviorDivergent {
		return fmt.Errorf("--behavior must be %q or %q, got %q", actors.BehaviorHonest, actors.BehaviorDivergent, *behavior)
	}

	imageEnv := os.Getenv("VC_IMAGE")
	if imageEnv == "" {
		return fmt.Errorf("VC_IMAGE environment variable is required and must be digest-pinned (repo@sha256:...)")
	}
	if _, err := sandbox.ParseImageRef(imageEnv); err != nil {
		return fmt.Errorf("invalid VC_IMAGE %q: %w", imageEnv, err)
	}

	pubKey, err := actors.ReadPublicKey(*key)
	if err != nil {
		return err
	}

	fcClient, err := freechains.NewClient(
		freechains.NewSerialRunner(freechains.NewExecRunner()),
		freechains.Config{
			Root:           *root,
			DefaultKey:     freechains.PrivateKeyPath(*key),
			CommandTimeout: 30 * time.Second,
		},
	)
	if err != nil {
		return fmt.Errorf("freechains client init failed: %w", err)
	}

	ipfsStore, err := ipfs.NewStore(ipfs.Config{
		APIAddr:        os.Getenv("VC_IPFS_API"),
		RequestTimeout: 30 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("ipfs store init failed: %w", err)
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
	sbRunner, err := sandbox.NewRunner(sandbox.Config{
		SocketURI:     os.Getenv("VC_PODMAN_SOCKET"),
		MaxConcurrent: 1,
		DefaultLimits: defaultLimits,
	})
	if err != nil {
		return fmt.Errorf("podman runner init failed: %w", err)
	}

	startupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := ipfsStore.Ping(startupCtx); err != nil {
		return fmt.Errorf("IPFS daemon unreachable (set VC_IPFS_API or start 'ipfs daemon'): %w", err)
	}
	if err := sbRunner.Ping(startupCtx); err != nil {
		return fmt.Errorf("Podman service unreachable (set VC_PODMAN_SOCKET or start 'podman system service --time=0'): %w", err)
	}
	if err := sbRunner.Cleanup(startupCtx); err != nil {
		return fmt.Errorf("Podman startup cleanup failed: %w", err)
	}

	var peers []string
	for _, p := range strings.Split(*peersCSV, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			peers = append(peers, p)
		}
	}

	nodeOpts := gen.NodeOptions{}
	nodeOpts.Network.Mode = gen.NetworkModeDisabled
	nodeOpts.Log.Level = gen.LogLevelError
	nodeOpts.Log.DefaultLogger.Disable = true

	nodeName := gen.Atom(fmt.Sprintf("%s@localhost", *id))
	node, err := ergo.StartNode(nodeName, nodeOpts)
	if err != nil {
		return fmt.Errorf("failed to start ergo node: %w", err)
	}
	defer node.StopForce()

	storePort := actors.NewIPFSAdapter(ipfsStore, ipfs.FetchLimits{
		MaxBytes: 10 * 1024 * 1024,
		MaxFiles: 100,
		MaxDepth: 8,
	})
	runnerPort := actors.NewSandboxAdapter(sbRunner, defaultLimits)
	chainPort := actors.NewFreechainsAdapter(fcClient, *chain, *key)

	bridgeAtom := gen.Atom("chain_bridge")
	workerAtom := gen.Atom("worker")
	submitterAtom := gen.Atom("submitter")

	targets := []gen.Atom{workerAtom}
	var doneCh chan struct{}
	var submitter *actors.SubmitterActor

	if *submit != "" {
		targets = append(targets, submitterAtom)
		doneCh = make(chan struct{})
	}

	wCfg := actors.WorkerConfig{
		NodeID:    *id,
		PublicKey: pubKey,
		Behavior:  *behavior,
		Bridge:    bridgeAtom,
	}
	if _, err := node.SpawnRegister(workerAtom, func() gen.ProcessBehavior {
		return actors.NewWorkerActor(storePort, runnerPort, wCfg)
	}, gen.ProcessOptions{}); err != nil {
		return fmt.Errorf("failed to spawn worker actor: %w", err)
	}

	if *submit != "" {
		sCfg := actors.SubmitterConfig{
			NodeID:      *id,
			InputFile:   *submit,
			ImageRef:    imageEnv,
			Replicas:    3,
			Quorum:      2,
			Reward:      1000,
			GracePeriod: time.Duration(*grace) * time.Second,
			Bridge:      bridgeAtom,
			ReportOut:   os.Stdout,
			Done:        doneCh,
		}
		submitter = actors.NewSubmitterActor(storePort, sCfg)
		if _, err := node.SpawnRegister(submitterAtom, func() gen.ProcessBehavior {
			return submitter
		}, gen.ProcessOptions{}); err != nil {
			return fmt.Errorf("failed to spawn submitter actor: %w", err)
		}
	}

	bCfg := actors.BridgeConfig{
		Peers:        peers,
		PollInterval: 500 * time.Millisecond,
		Targets:      targets,
	}
	if _, err := node.SpawnRegister(bridgeAtom, func() gen.ProcessBehavior {
		return actors.NewChainBridge(chainPort, bCfg)
	}, gen.ProcessOptions{}); err != nil {
		return fmt.Errorf("failed to spawn chain bridge actor: %w", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	if doneCh != nil {
		select {
		case <-doneCh:
			rep := submitter.Report()
			return rep.Err
		case sig := <-sigCh:
			return fmt.Errorf("interrupted by signal %v", sig)
		}
	}

	<-sigCh
	return nil
}
