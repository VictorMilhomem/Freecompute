package sandbox

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/containers/podman/v5/pkg/specgen"
)

type mockEngine struct {
	infoFunc            func(ctx context.Context) (EngineInfo, error)
	imageExistsFunc     func(ctx context.Context, ref string) (bool, error)
	pullImageFunc       func(ctx context.Context, ref string) error
	createContainerFunc func(ctx context.Context, s *specgen.SpecGenerator) (string, error)
	startFunc           func(ctx context.Context, id string) error
	waitFunc            func(ctx context.Context, id string) (int32, error)
	killFunc            func(ctx context.Context, id string, signal string) error
	logsFunc            func(ctx context.Context, id string, maxBytes int64) (stdout, stderr []byte, truncated bool, err error)
	inspectFunc         func(ctx context.Context, id string) (ContainerState, error)
	removeFunc          func(ctx context.Context, id string, force, volumes bool) error
	listByLabelFunc     func(ctx context.Context, label string) ([]string, error)

	lastCreatedSpec *specgen.SpecGenerator
	removedIDs      []string
	killedIDs       []string
}

func (m *mockEngine) Info(ctx context.Context) (EngineInfo, error) {
	if m.infoFunc != nil {
		return m.infoFunc(ctx)
	}
	return EngineInfo{Rootless: true}, nil
}

func (m *mockEngine) ImageExists(ctx context.Context, ref string) (bool, error) {
	if m.imageExistsFunc != nil {
		return m.imageExistsFunc(ctx, ref)
	}
	return true, nil
}

func (m *mockEngine) PullImage(ctx context.Context, ref string) error {
	if m.pullImageFunc != nil {
		return m.pullImageFunc(ctx, ref)
	}
	return nil
}

func (m *mockEngine) CreateContainer(ctx context.Context, s *specgen.SpecGenerator) (string, error) {
	m.lastCreatedSpec = s
	if m.createContainerFunc != nil {
		return m.createContainerFunc(ctx, s)
	}
	return "container-12345", nil
}

func (m *mockEngine) Start(ctx context.Context, id string) error {
	if m.startFunc != nil {
		return m.startFunc(ctx, id)
	}
	return nil
}

func (m *mockEngine) Wait(ctx context.Context, id string) (int32, error) {
	if m.waitFunc != nil {
		return m.waitFunc(ctx, id)
	}
	return 0, nil
}

func (m *mockEngine) Kill(ctx context.Context, id string, signal string) error {
	m.killedIDs = append(m.killedIDs, id)
	if m.killFunc != nil {
		return m.killFunc(ctx, id, signal)
	}
	return nil
}

func (m *mockEngine) Logs(ctx context.Context, id string, maxBytes int64) ([]byte, []byte, bool, error) {
	if m.logsFunc != nil {
		return m.logsFunc(ctx, id, maxBytes)
	}
	return []byte("stdout"), []byte("stderr"), false, nil
}

func (m *mockEngine) Inspect(ctx context.Context, id string) (ContainerState, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, id)
	}
	return ContainerState{ExitCode: 0, OOMKilled: false, Running: false}, nil
}

func (m *mockEngine) Remove(ctx context.Context, id string, force, volumes bool) error {
	m.removedIDs = append(m.removedIDs, id)
	if m.removeFunc != nil {
		return m.removeFunc(ctx, id, force, volumes)
	}
	return nil
}

func (m *mockEngine) ListByLabel(ctx context.Context, label string) ([]string, error) {
	if m.listByLabelFunc != nil {
		return m.listByLabelFunc(ctx, label)
	}
	return []string{"orphan-1", "orphan-2"}, nil
}

type staticPolicy struct {
	allowed map[ImageRef]bool
}

func (p *staticPolicy) Allow(ref ImageRef) error {
	if !p.allowed[ref] {
		return errors.New("image disallowed by static policy")
	}
	return nil
}

func setupTestDirs(t *testing.T) (string, string) {
	t.Helper()
	inDir := filepath.Join(t.TempDir(), "input")
	outDir := filepath.Join(t.TempDir(), "output")
	_ = filepath.Walk(inDir, nil)
	_ = filepath.Walk(outDir, nil)
	if err := osMkdir(inDir); err != nil {
		t.Fatal(err)
	}
	if err := osMkdir(outDir); err != nil {
		t.Fatal(err)
	}
	return inDir, outDir
}

func osMkdir(path string) error {
	return filepath.Walk(path, nil) // no-op fallback, real mkdir below
}

func TestRunner(t *testing.T) {
	validImage := ImageRef("docker.io/library/alpine@sha256:e7d88de73db3d3a9b58339f1f4096306354fa13ac2048fe55e3d09d6b279e430")
	ctx := context.Background()

	defaultCfg := Config{
		RequireRootless: true,
		MaxConcurrent:   1,
		DefaultLimits: Limits{
			CPUs:           1.0,
			MemoryBytes:    256 * 1024 * 1024,
			Timeout:        10 * time.Second,
			PidsLimit:      50,
			TmpfsBytes:     32 * 1024 * 1024,
			MaxOutputBytes: 10 * 1024 * 1024,
			MaxOutputFiles: 50,
		},
		HardLimits: Limits{
			CPUs:           2.0,
			MemoryBytes:    1024 * 1024 * 1024,
			Timeout:        30 * time.Second,
			PidsLimit:      100,
			TmpfsBytes:     64 * 1024 * 1024,
			MaxOutputBytes: 20 * 1024 * 1024,
			MaxOutputFiles: 100,
		},
	}

	t.Run("mandatory isolation settings in created spec", func(t *testing.T) {
		inDir := t.TempDir()
		outDir := t.TempDir()

		mock := &mockEngine{}
		runner := newRunnerWithEngine(mock, defaultCfg)

		spec := Spec{
			Image:     validImage,
			Cmd:       []string{"echo", "hi"},
			InputDir:  inDir,
			OutputDir: outDir,
			Labels:    map[string]string{"job_id": "job-123"},
		}

		res, err := runner.Run(ctx, spec)
		if err != nil {
			t.Fatalf("unexpected error running spec: %v", err)
		}
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", res.ExitCode)
		}

		s := mock.lastCreatedSpec
		if s == nil {
			t.Fatal("no spec was passed to CreateContainer")
		}

		// Verify rootless user
		if s.User != "65534:65534" {
			t.Errorf("User mismatch: got %s, want 65534:65534", s.User)
		}
		// Verify read-only rootfs
		if s.ReadOnlyFilesystem == nil || !*s.ReadOnlyFilesystem {
			t.Error("expected ReadOnlyFilesystem=true")
		}
		// Verify no-new-privileges
		if s.NoNewPrivileges == nil || !*s.NoNewPrivileges {
			t.Error("expected NoNewPrivileges=true")
		}
		// Verify cap drop ALL
		if !reflect.DeepEqual(s.CapDrop, []string{"ALL"}) {
			t.Errorf("CapDrop mismatch: %v", s.CapDrop)
		}
		// Verify not privileged
		if s.Privileged != nil && *s.Privileged {
			t.Error("Privileged must not be true")
		}
		// Verify no network
		if s.NetNS.NSMode != specgen.NoNetwork {
			t.Errorf("NetNS mismatch: %v", s.NetNS.NSMode)
		}
		// Verify managed label
		if s.Labels["vc.managed"] != "true" || s.Labels["job_id"] != "job-123" {
			t.Errorf("Labels mismatch: %+v", s.Labels)
		}

		// Verify mounts: /input (ro), /output (rw), /tmp (tmpfs)
		var hasInput, hasOutput, hasTmp bool
		for _, m := range s.Mounts {
			switch m.Destination {
			case "/input":
				hasInput = true
				if m.Source != inDir || !reflect.DeepEqual(m.Options, []string{"ro", "nodev", "nosuid", "noexec", "rbind"}) {
					t.Errorf("unexpected /input mount: %+v", m)
				}
			case "/output":
				hasOutput = true
				if m.Source != outDir || !reflect.DeepEqual(m.Options, []string{"rw", "nodev", "nosuid", "noexec", "rbind"}) {
					t.Errorf("unexpected /output mount: %+v", m)
				}
			case "/tmp":
				hasTmp = true
				if m.Type != "tmpfs" {
					t.Errorf("unexpected /tmp mount type: %s", m.Type)
				}
			}
		}
		if !hasInput || !hasOutput || !hasTmp {
			t.Errorf("missing mount: in=%v out=%v tmp=%v", hasInput, hasOutput, hasTmp)
		}

		// Verify container was removed
		if len(mock.removedIDs) != 1 || mock.removedIDs[0] != "container-12345" {
			t.Errorf("container removal mismatch: %v", mock.removedIDs)
		}
	})

	t.Run("image policy rejection", func(t *testing.T) {
		inDir := t.TempDir()
		outDir := t.TempDir()

		cfg := defaultCfg
		cfg.ImagePolicy = &staticPolicy{allowed: map[ImageRef]bool{}} // allows nothing

		mock := &mockEngine{}
		runner := newRunnerWithEngine(mock, cfg)

		spec := Spec{
			Image:     validImage,
			Cmd:       []string{"ls"},
			InputDir:  inDir,
			OutputDir: outDir,
		}

		_, err := runner.Run(ctx, spec)
		if err == nil {
			t.Fatal("expected error from image policy, got nil")
		}
		if !errors.Is(err, ErrImageNotAllowed) {
			t.Errorf("expected ErrImageNotAllowed, got %v", err)
		}
	})

	t.Run("limits above ceilings rejected", func(t *testing.T) {
		inDir := t.TempDir()
		outDir := t.TempDir()

		mock := &mockEngine{}
		runner := newRunnerWithEngine(mock, defaultCfg)

		spec := Spec{
			Image:     validImage,
			Cmd:       []string{"ls"},
			InputDir:  inDir,
			OutputDir: outDir,
			Limits:    Limits{CPUs: 5.0}, // ceiling is 2.0
		}

		_, err := runner.Run(ctx, spec)
		if err == nil {
			t.Fatal("expected ErrLimitsExceeded, got nil")
		}
		if !errors.Is(err, ErrLimitsExceeded) {
			t.Errorf("expected ErrLimitsExceeded, got %v", err)
		}
	})

	t.Run("timeout kills container and returns ErrTimeout with logs", func(t *testing.T) {
		inDir := t.TempDir()
		outDir := t.TempDir()

		mock := &mockEngine{
			waitFunc: func(ctx context.Context, id string) (int32, error) {
				<-ctx.Done()
				return -1, ctx.Err()
			},
			logsFunc: func(ctx context.Context, id string, maxBytes int64) ([]byte, []byte, bool, error) {
				return []byte("timeout stdout"), []byte("timeout stderr"), false, nil
			},
		}

		cfg := defaultCfg
		runner := newRunnerWithEngine(mock, cfg)

		spec := Spec{
			Image:     validImage,
			Cmd:       []string{"sleep", "100"},
			InputDir:  inDir,
			OutputDir: outDir,
			Limits:    Limits{Timeout: 10 * time.Millisecond},
		}

		res, err := runner.Run(ctx, spec)
		if err == nil {
			t.Fatal("expected ErrTimeout, got nil")
		}
		if !errors.Is(err, ErrTimeout) {
			t.Errorf("expected ErrTimeout, got %v", err)
		}
		if string(res.Stdout) != "timeout stdout" || string(res.Stderr) != "timeout stderr" {
			t.Errorf("logs mismatch: stdout=%q, stderr=%q", string(res.Stdout), string(res.Stderr))
		}
		if len(mock.killedIDs) != 1 {
			t.Errorf("expected container to be killed, killedIDs: %v", mock.killedIDs)
		}
		if len(mock.removedIDs) != 1 {
			t.Errorf("expected container to be removed, removedIDs: %v", mock.removedIDs)
		}
	})

	t.Run("OOM detection maps to ErrOOMKilled", func(t *testing.T) {
		inDir := t.TempDir()
		outDir := t.TempDir()

		mock := &mockEngine{
			waitFunc: func(ctx context.Context, id string) (int32, error) {
				return 137, nil
			},
			inspectFunc: func(ctx context.Context, id string) (ContainerState, error) {
				return ContainerState{ExitCode: 137, OOMKilled: true}, nil
			},
		}

		runner := newRunnerWithEngine(mock, defaultCfg)
		spec := Spec{
			Image:     validImage,
			Cmd:       []string{"oom-app"},
			InputDir:  inDir,
			OutputDir: outDir,
		}

		res, err := runner.Run(ctx, spec)
		if err == nil {
			t.Fatal("expected ErrOOMKilled, got nil")
		}
		if !errors.Is(err, ErrOOMKilled) {
			t.Errorf("expected ErrOOMKilled, got %v", err)
		}
		if res.ExitCode != 137 {
			t.Errorf("expected exit code 137, got %d", res.ExitCode)
		}
	})

	t.Run("nonzero exit code returned without error", func(t *testing.T) {
		inDir := t.TempDir()
		outDir := t.TempDir()

		mock := &mockEngine{
			waitFunc: func(ctx context.Context, id string) (int32, error) {
				return 42, nil
			},
			inspectFunc: func(ctx context.Context, id string) (ContainerState, error) {
				return ContainerState{ExitCode: 42, OOMKilled: false}, nil
			},
		}

		runner := newRunnerWithEngine(mock, defaultCfg)
		spec := Spec{
			Image:     validImage,
			Cmd:       []string{"false"},
			InputDir:  inDir,
			OutputDir: outDir,
		}

		res, err := runner.Run(ctx, spec)
		if err != nil {
			t.Fatalf("nonzero exit code should not return Go error: %v", err)
		}
		if res.ExitCode != 42 {
			t.Errorf("expected exit code 42, got %d", res.ExitCode)
		}
	})

	t.Run("concurrency limit blocks and releases", func(t *testing.T) {
		inDir := t.TempDir()
		outDir := t.TempDir()

		blockCh := make(chan struct{})
		mock := &mockEngine{
			waitFunc: func(ctx context.Context, id string) (int32, error) {
				<-blockCh
				return 0, nil
			},
		}

		cfg := defaultCfg
		cfg.MaxConcurrent = 1
		runner := newRunnerWithEngine(mock, cfg)

		spec := Spec{
			Image:     validImage,
			Cmd:       []string{"task"},
			InputDir:  inDir,
			OutputDir: outDir,
		}

		go func() {
			_, _ = runner.Run(context.Background(), spec)
		}()

		// Give first run time to acquire semaphore
		time.Sleep(20 * time.Millisecond)

		// Second run with tight timeout should block and timeout
		shortCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_, err := runner.Run(shortCtx, spec)
		if err == nil {
			t.Fatal("expected second run to block and timeout, got nil")
		}

		close(blockCh)
	})

	t.Run("Cleanup removes only labelled containers", func(t *testing.T) {
		var queriedLabel string
		mock := &mockEngine{
			listByLabelFunc: func(ctx context.Context, label string) ([]string, error) {
				queriedLabel = label
				return []string{"orphan-1", "orphan-2"}, nil
			},
		}

		runner := newRunnerWithEngine(mock, defaultCfg)
		if err := runner.Cleanup(ctx); err != nil {
			t.Fatalf("unexpected cleanup error: %v", err)
		}

		if queriedLabel != "vc.managed=true" {
			t.Errorf("queried label mismatch: %q", queriedLabel)
		}
		if len(mock.removedIDs) != 2 || mock.removedIDs[0] != "orphan-1" || mock.removedIDs[1] != "orphan-2" {
			t.Errorf("removed containers mismatch: %v", mock.removedIDs)
		}
	})

	t.Run("Ping fails with ErrNotRootless if engine is root", func(t *testing.T) {
		mock := &mockEngine{
			infoFunc: func(ctx context.Context) (EngineInfo, error) {
				return EngineInfo{Rootless: false}, nil
			},
		}

		cfg := defaultCfg
		cfg.RequireRootless = true
		runner := newRunnerWithEngine(mock, cfg)

		if err := runner.Ping(ctx); !errors.Is(err, ErrNotRootless) {
			t.Errorf("expected ErrNotRootless, got %v", err)
		}
	})
}
