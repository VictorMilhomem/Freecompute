package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/VictorMilhomem/freecompute/pkg/freecompute"
)

type cliFlags struct {
	id           string
	root         string
	key          string
	chain        string
	peers        []string
	stateDir     string
	image        string
	input        string
	out          string
	replicas     uint
	quorum       uint
	reward       int64
	commitWindow uint
	revealWindow uint
	allowOwnJobs bool
	timeout      time.Duration
}

func main() {
	opts, err := parseFlags(os.Args[1:], os.Getenv("VC_IMAGE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "flag error: %v\n", err)
		os.Exit(2)
	}
	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags(args []string, envImage string) (cliFlags, error) {
	var opts cliFlags
	var peersCSV string

	fs := flag.NewFlagSet("example-simple", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	fs.StringVar(&opts.id, "id", "", "node id")
	fs.StringVar(&opts.root, "root", "", "Freechains root")
	fs.StringVar(&opts.key, "key", "", "private key (ed25519)")
	fs.StringVar(&opts.chain, "chain", "#vc", "Freechains chain")
	fs.StringVar(&peersCSV, "peers", "", "comma-separated host:port peers")
	fs.StringVar(&opts.stateDir, "state-dir", "", "worker state directory (optional)")
	fs.StringVar(&opts.image, "image", envImage, "digest-pinned image (repo@sha256:...)")
	fs.StringVar(&opts.input, "input", "", "input file")
	fs.StringVar(&opts.out, "out", "", "directory where the winning result is fetched")
	fs.UintVar(&opts.replicas, "replicas", 3, "number of replicas")
	fs.UintVar(&opts.quorum, "quorum", 2, "minimum quorum")
	fs.Int64Var(&opts.reward, "reward", 1000, "reward in reputation points")
	fs.UintVar(&opts.commitWindow, "commit-window", 20, "commit window in chain positions")
	fs.UintVar(&opts.revealWindow, "reveal-window", 20, "reveal window in chain positions")
	fs.BoolVar(&opts.allowOwnJobs, "allow-own-jobs", false, "let this node also work on its own job")
	fs.DurationVar(&opts.timeout, "timeout", 3*time.Minute, "overall deadline for Wait")

	if err := fs.Parse(args); err != nil {
		return cliFlags{}, err
	}
	if fs.NArg() > 0 {
		return cliFlags{}, fmt.Errorf("unexpected positional arguments: %v", fs.Args())
	}
	if opts.id == "" {
		return cliFlags{}, errors.New("--id is required")
	}
	if opts.root == "" {
		return cliFlags{}, errors.New("--root is required")
	}
	if opts.key == "" {
		return cliFlags{}, errors.New("--key is required")
	}
	if opts.image == "" {
		return cliFlags{}, errors.New("--image (or VC_IMAGE) is required")
	}
	if opts.input == "" {
		return cliFlags{}, errors.New("--input is required")
	}
	if opts.replicas < 1 || opts.quorum < 1 || opts.quorum > opts.replicas {
		return cliFlags{}, fmt.Errorf("invalid replicas=%d quorum=%d", opts.replicas, opts.quorum)
	}
	if opts.commitWindow == 0 || opts.revealWindow == 0 {
		return cliFlags{}, errors.New("--commit-window and --reveal-window must be > 0")
	}
	if opts.timeout <= 0 {
		return cliFlags{}, errors.New("--timeout must be > 0")
	}

	for _, p := range strings.Split(peersCSV, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			opts.peers = append(opts.peers, p)
		}
	}
	return opts, nil
}

func run(opts cliFlags) error {
	ctx := context.Background()
	// Route structured logs and progress events to stderr so stdout carries only the machine-parsed report.
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	node, err := freecompute.NewNode(freecompute.Config{
		ID:       opts.id,
		Root:     opts.root,
		Key:      opts.key,
		Chain:    opts.chain,
		Peers:    opts.peers,
		StateDir: opts.stateDir,
	}, freecompute.WithLogger(logger))
	if err != nil {
		return err
	}

	if err := node.Start(ctx); err != nil {
		return err
	}
	defer func() {
		_ = node.Stop(context.Background())
	}()

	if opts.allowOwnJobs {
		if err := node.Serve(freecompute.WorkerPolicy{
			AllowImages:   []string{opts.image},
			AllowOwnJobs:  true,
			MaxConcurrent: 1,
		}); err != nil {
			return err
		}
	}

	inputBase := filepath.Base(opts.input)
	cmd := []string{"sh", "-c", fmt.Sprintf("wc -w < /input/%s > /output/result.txt", inputBase)}

	job, err := node.Submit(ctx, freecompute.JobSpec{
		Image:        opts.image,
		Cmd:          cmd,
		Input:        freecompute.FromFile(opts.input),
		Replicas:     uint32(opts.replicas),
		Quorum:       uint32(opts.quorum),
		Reward:       opts.reward,
		CommitWindow: uint32(opts.commitWindow),
		RevealWindow: uint32(opts.revealWindow),
	})
	if err != nil {
		return err
	}

	stopEvents := make(chan struct{})
	defer close(stopEvents)
	go logEvents(job.Events(), stopEvents)

	waitCtx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	res, err := job.Wait(waitCtx)
	if err != nil {
		return err
	}

	outDir := opts.out
	if outDir == "" {
		tmpOut, err := os.MkdirTemp("", "example-simple-out-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmpOut)
		outDir = tmpOut
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if err := node.Fetch(ctx, res.OutputCID, outDir); err != nil {
		return err
	}

	raw, err := os.ReadFile(filepath.Join(outDir, "result.txt"))
	if err != nil {
		return err
	}

	_, err = fmt.Fprint(os.Stdout, formatReport(res, strings.TrimSpace(string(raw))))
	return err
}

func logEvents(events <-chan freecompute.Event, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			switch e := ev.(type) {
			case freecompute.Announced:
				fmt.Fprintf(os.Stderr, "event=announced job_id=%s\n", e.JobID)
			case freecompute.Committed:
				fmt.Fprintf(os.Stderr, "event=committed job_id=%s worker=%s\n", e.JobID, e.Worker)
			case freecompute.RevealPhaseOpened:
				fmt.Fprintf(os.Stderr, "event=reveal_phase_opened job_id=%s\n", e.JobID)
			case freecompute.Revealed:
				fmt.Fprintf(os.Stderr, "event=revealed job_id=%s worker=%s\n", e.JobID, e.Worker)
			case freecompute.Finalized:
				fmt.Fprintf(os.Stderr, "event=finalized job_id=%s accepted_hash=%s\n", e.Result.JobID, e.Result.AcceptedHash)
			case freecompute.Expired:
				fmt.Fprintf(os.Stderr, "event=expired job_id=%s\n", e.JobID)
			}
		}
	}
}

func formatReport(res freecompute.Result, resultText string) string {
	return fmt.Sprintf(
		"JOB_ID=%s\nACCEPTED_HASH=%s\nVERIFIED=true\nRESULT=%s\nWINNERS=%s\nDIVERGENT=%s\nNO_REVEAL=%s\n",
		res.JobID,
		res.AcceptedHash,
		strings.TrimSpace(resultText),
		strings.Join(res.Winners, ","),
		strings.Join(res.Divergent, ","),
		strings.Join(res.NoReveal, ","),
	)
}
