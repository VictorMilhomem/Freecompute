package sandbox

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/containers/podman/v5/pkg/specgen"
	specs "github.com/opencontainers/runtime-spec/specs-go"
)

// EngineInfo contains basic state reported by the container engine.
type EngineInfo struct {
	Rootless bool
}

// ContainerState captures runtime state after execution.
type ContainerState struct {
	ExitCode  int32
	OOMKilled bool
	Running   bool
}

// engine defines the private port isolating Podman bindings.
type engine interface {
	Info(ctx context.Context) (EngineInfo, error)
	ImageExists(ctx context.Context, ref string) (bool, error)
	PullImage(ctx context.Context, ref string) error
	CreateContainer(ctx context.Context, s *specgen.SpecGenerator) (string, error)
	Start(ctx context.Context, id string) error
	Wait(ctx context.Context, id string) (int32, error)
	Kill(ctx context.Context, id string, signal string) error
	Logs(ctx context.Context, id string, maxBytes int64) (stdout, stderr []byte, truncated bool, err error)
	Inspect(ctx context.Context, id string) (ContainerState, error)
	Remove(ctx context.Context, id string, force, volumes bool) error
	ListByLabel(ctx context.Context, label string) ([]string, error)
}

type runner struct {
	engine engine
	cfg    Config
	sem    chan struct{}
}

func newRunnerWithEngine(eng engine, cfg Config) *runner {
	return &runner{
		engine: eng,
		cfg:    cfg,
		sem:    make(chan struct{}, cfg.MaxConcurrent),
	}
}

func (r *runner) Ping(ctx context.Context) error {
	info, err := r.engine.Info(ctx)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	if r.cfg.RequireRootless && !info.Rootless {
		return ErrNotRootless
	}
	return nil
}

func (r *runner) EnsureImage(ctx context.Context, ref ImageRef) error {
	exists, err := r.engine.ImageExists(ctx, ref.String())
	if err != nil {
		return fmt.Errorf("%w: failed to check image presence: %v", ErrEngineUnavailable, err)
	}
	if exists {
		return nil
	}

	if r.cfg.OfflineImages {
		return fmt.Errorf("%w: image %s missing in offline mode", ErrImageNotAllowed, ref)
	}

	if err := r.engine.PullImage(ctx, ref.String()); err != nil {
		return fmt.Errorf("%w: failed to pull image %s: %v", ErrEngineUnavailable, ref, err)
	}
	return nil
}

func (r *runner) Cleanup(ctx context.Context) error {
	ids, err := r.engine.ListByLabel(ctx, "vc.managed=true")
	if err != nil {
		return fmt.Errorf("%w: failed to list managed containers: %v", ErrEngineUnavailable, err)
	}

	var firstErr error
	for _, id := range ids {
		if rmErr := r.engine.Remove(ctx, id, true, true); rmErr != nil {
			if firstErr == nil {
				firstErr = rmErr
			}
		}
	}
	return firstErr
}

func (r *runner) Run(ctx context.Context, spec Spec) (Result, error) {
	// 1. Validate spec
	if err := spec.Validate(r.cfg.ImagePolicy, r.cfg.DefaultLimits, r.cfg.HardLimits); err != nil {
		return Result{}, err
	}

	// 2. Concurrency semaphore acquisition
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}

	// 3. Ensure image is present
	if err := r.EnsureImage(ctx, spec.Image); err != nil {
		return Result{}, err
	}

	// 4. Build specgen with mandatory isolation settings
	s := specgen.NewSpecGenerator(spec.Image.String(), false)
	trueVal := true
	falseVal := false

	s.Command = spec.Cmd
	s.User = "65534:65534"
	s.ReadOnlyFilesystem = &trueVal
	s.NoNewPrivileges = &trueVal
	s.CapDrop = []string{"ALL"}
	s.Privileged = &falseVal
	s.NetNS = specgen.Namespace{NSMode: specgen.NoNetwork}

	// Managed labels
	s.Labels = make(map[string]string)
	for k, v := range spec.Labels {
		s.Labels[k] = v
	}
	s.Labels["vc.managed"] = "true"

	// Mounts: InputDir ro, OutputDir rw, /tmp tmpfs
	s.Mounts = []specs.Mount{
		{
			Source:      spec.InputDir,
			Destination: "/input",
			Type:        "bind",
			Options:     []string{"ro", "nodev", "nosuid", "noexec", "rbind"},
		},
		{
			Source:      spec.OutputDir,
			Destination: "/output",
			Type:        "bind",
			Options:     []string{"rw", "nodev", "nosuid", "noexec", "rbind"},
		},
	}

	tmpfsOpts := []string{"nodev", "noexec", "nosuid"}
	if spec.Limits.TmpfsBytes > 0 {
		tmpfsOpts = append(tmpfsOpts, fmt.Sprintf("size=%d", spec.Limits.TmpfsBytes))
	}
	s.Mounts = append(s.Mounts, specs.Mount{
		Destination: "/tmp",
		Type:        "tmpfs",
		Options:     tmpfsOpts,
	})

	// Resource limits
	s.ResourceLimits = &specs.LinuxResources{}
	if spec.Limits.PidsLimit > 0 {
		s.ResourceLimits.Pids = &specs.LinuxPids{Limit: spec.Limits.PidsLimit}
	}
	if spec.Limits.MemoryBytes > 0 {
		mem := spec.Limits.MemoryBytes
		s.ResourceLimits.Memory = &specs.LinuxMemory{Limit: &mem}
	}
	if spec.Limits.CPUs > 0 {
		period := uint64(100000)
		quota := int64(spec.Limits.CPUs * 100000)
		s.ResourceLimits.CPU = &specs.LinuxCPU{Period: &period, Quota: &quota}
	}

	containerID, err := r.engine.CreateContainer(ctx, s)
	if err != nil {
		return Result{}, fmt.Errorf("%w: failed to create container: %v", ErrEngineUnavailable, err)
	}

	// 9. Always remove container in defer using a fresh context
	defer func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), r.cfg.CleanupTimeout)
		defer cleanCancel()
		_ = r.engine.Remove(cleanCtx, containerID, true, true)
	}()

	startTime := time.Now()

	// 4. Start container
	if err := r.engine.Start(ctx, containerID); err != nil {
		return Result{}, fmt.Errorf("%w: failed to start container: %v", ErrEngineUnavailable, err)
	}

	// 4. Wait under timeout
	waitCtx := ctx
	var waitCancel context.CancelFunc
	if spec.Limits.Timeout > 0 {
		waitCtx, waitCancel = context.WithTimeout(ctx, spec.Limits.Timeout)
		defer waitCancel()
	}

	waitChan := make(chan struct {
		code int32
		err  error
	}, 1)

	go func() {
		code, wErr := r.engine.Wait(waitCtx, containerID)
		waitChan <- struct {
			code int32
			err  error
		}{code: code, err: wErr}
	}()

	var exitCode int32
	select {
	case res := <-waitChan:
		if res.err != nil {
			err = res.err
		} else {
			exitCode = res.code
		}
	case <-waitCtx.Done():
		err = waitCtx.Err()
	}

	duration := time.Since(startTime)

	// 5. On timeout or cancellation: kill, inspect, get logs, return ErrTimeout
	if err != nil && (errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)) {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), r.cfg.CleanupTimeout)
		defer cleanCancel()

		_ = r.engine.Kill(cleanCtx, containerID, "SIGKILL")
		stdout, stderr, truncated, _ := r.engine.Logs(cleanCtx, containerID, r.cfg.LogLimitBytes)

		return Result{
			ExitCode:      -1,
			Duration:      duration,
			Stdout:        stdout,
			Stderr:        stderr,
			LogsTruncated: truncated,
		}, ErrTimeout
	}

	// Fresh context for post-execution inspections
	cleanCtx, cleanCancel := context.WithTimeout(context.Background(), r.cfg.CleanupTimeout)
	defer cleanCancel()

	// 6. Check OOM
	inspectState, inspectErr := r.engine.Inspect(cleanCtx, containerID)
	stdout, stderr, truncated, _ := r.engine.Logs(cleanCtx, containerID, r.cfg.LogLimitBytes)

	if inspectErr == nil && inspectState.OOMKilled {
		return Result{
			ExitCode:      inspectState.ExitCode,
			Duration:      duration,
			Stdout:        stdout,
			Stderr:        stderr,
			LogsTruncated: truncated,
		}, ErrOOMKilled
	}

	if err != nil {
		return Result{
			ExitCode:      exitCode,
			Duration:      duration,
			Stdout:        stdout,
			Stderr:        stderr,
			LogsTruncated: truncated,
		}, err
	}

	// 8. Verify output directory
	outBytes, outFiles, verifyErr := VerifyOutputDir(spec.OutputDir, spec.Limits.MaxOutputBytes, spec.Limits.MaxOutputFiles)
	res := Result{
		ExitCode:      exitCode,
		Duration:      duration,
		Stdout:        stdout,
		Stderr:        stderr,
		LogsTruncated: truncated,
		OutputBytes:   outBytes,
		OutputFiles:   outFiles,
	}

	if verifyErr != nil {
		return res, verifyErr
	}

	return res, nil
}
