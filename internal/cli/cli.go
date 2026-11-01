package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/VictorMilhomem/freecompute/internal/sandbox"
	"github.com/VictorMilhomem/freecompute/pkg/freecompute"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(val string) error {
	for _, part := range strings.Split(val, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

func NewDefaultRunner() (sandbox.Runner, error) {
	defaultLimits := sandbox.Limits{
		CPUs:           1.0,
		MemoryBytes:    256 * 1024 * 1024,
		Timeout:        60 * time.Second,
		PidsLimit:      100,
		TmpfsBytes:     64 * 1024 * 1024,
		MaxOutputBytes: 10 * 1024 * 1024,
		MaxOutputFiles: 100,
	}
	return sandbox.NewRunner(sandbox.Config{
		SocketURI:     os.Getenv("VC_PODMAN_SOCKET"),
		MaxConcurrent: 1,
		DefaultLimits: defaultLimits,
	})
}

func Run(args []string, opts ...freecompute.Option) error {
	if len(args) > 0 && args[0] == "run" {
		args = args[1:]
	}

	fs := flag.NewFlagSet("vcnode", flag.ContinueOnError)
	id := fs.String("id", "", "node alias (e.g. alice, bob, carol)")
	root := fs.String("root", "", "freechains root directory")
	key := fs.String("key", "", "private key path")
	chain := fs.String("chain", "#vc", "freechains chain alias")
	peersCSV := fs.String("peers", "", "comma-separated peer host:port list")
	stateDir := fs.String("state-dir", "", "worker state directory (default <root>/vc-state)")
	submit := fs.String("submit", "", "input file path to submit as a job")
	allowAnyImage := fs.Bool("allow-any-image", false, "allow any digest-pinned image (dev only)")
	allowOwnJobs := fs.Bool("allow-own-jobs", false, "allow working on jobs submitted by this node")
	var allowImages stringSliceFlag
	fs.Var(&allowImages, "allow-image", "allowed digest-pinned image (repeatable)")

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

	imageEnv := os.Getenv("VC_IMAGE")
	if imageEnv == "" {
		return fmt.Errorf("VC_IMAGE environment variable is required and must be digest-pinned (repo@sha256:...)")
	}

	var peers []string
	for _, p := range strings.Split(*peersCSV, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			peers = append(peers, p)
		}
	}

	node, err := freecompute.NewNode(freecompute.Config{
		ID:       *id,
		Root:     *root,
		Key:      *key,
		Chain:    *chain,
		Peers:    peers,
		StateDir: *stateDir,
	}, opts...)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := node.Start(ctx); err != nil {
		return err
	}
	defer func() {
		_ = node.Stop(context.Background())
	}()

	if err := node.Serve(freecompute.WorkerPolicy{
		AllowImages:   allowImages,
		AllowAnyImage: *allowAnyImage,
		AllowOwnJobs:  *allowOwnJobs,
		MaxConcurrent: 1,
	}); err != nil {
		return err
	}

	if *submit == "" {
		<-ctx.Done()
		return nil
	}

	job, err := node.Submit(ctx, freecompute.JobSpec{
		Image:    imageEnv,
		Cmd:      []string{"sh", "-c", "wc -w < /input/data.txt > /output/result.txt"},
		Input:    freecompute.FromFile(*submit),
		Replicas: 3,
		Quorum:   2,
		Reward:   1000,
	})
	if err != nil {
		return err
	}

	res, err := job.Wait(ctx)
	if err != nil {
		fmt.Printf("JOB_ID=%s\nACCEPTED_HASH=%s\nVERIFIED=false\nRESULT=\nWINNERS=%s\nDIVERGENT=%s\nNO_REVEAL=%s\n",
			res.JobID,
			res.AcceptedHash,
			strings.Join(res.Winners, ","),
			strings.Join(res.Divergent, ","),
			strings.Join(res.NoReveal, ","),
		)
		return err
	}

	outDir, err := os.MkdirTemp("", "vcnode-result-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(outDir)

	if err := node.Fetch(ctx, res.OutputCID, outDir); err != nil {
		return err
	}
	rawResult, err := os.ReadFile(filepath.Join(outDir, "result.txt"))
	if err != nil {
		return err
	}
	resultStr := strings.TrimSpace(string(rawResult))

	fmt.Printf("JOB_ID=%s\nACCEPTED_HASH=%s\nVERIFIED=true\nRESULT=%s\nWINNERS=%s\nDIVERGENT=%s\nNO_REVEAL=%s\n",
		res.JobID,
		res.AcceptedHash,
		resultStr,
		strings.Join(res.Winners, ","),
		strings.Join(res.Divergent, ","),
		strings.Join(res.NoReveal, ","),
	)
	return nil
}
