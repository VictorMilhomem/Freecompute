package sandbox

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Runner orchestrates isolated container executions.
type Runner interface {
	Run(ctx context.Context, spec Spec) (Result, error)
	EnsureImage(ctx context.Context, ref ImageRef) error
	Cleanup(ctx context.Context) error
	Ping(ctx context.Context) error
}

// Result describes the outcome of a completed container execution.
type Result struct {
	ExitCode      int32
	Duration      time.Duration
	Stdout        []byte
	Stderr        []byte
	LogsTruncated bool
	OutputBytes   int64
	OutputFiles   int
}

// Config configures the container runner.
type Config struct {
	SocketURI       string
	RequireRootless bool
	MaxConcurrent   int
	DefaultLimits   Limits
	HardLimits      Limits
	LogLimitBytes   int64
	CleanupTimeout  time.Duration
	OfflineImages   bool
	ImagePolicy     ImagePolicy
}

// Validate checks and applies configuration defaults.
func (c *Config) Validate() error {
	if c.SocketURI == "" {
		runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
		if runtimeDir != "" {
			c.SocketURI = fmt.Sprintf("unix://%s/podman/podman.sock", runtimeDir)
		} else {
			c.SocketURI = fmt.Sprintf("unix:///run/user/%d/podman/podman.sock", os.Getuid())
		}
	}

	if c.MaxConcurrent <= 0 {
		c.MaxConcurrent = 1
	}

	if c.LogLimitBytes <= 0 {
		c.LogLimitBytes = 1024 * 1024 // 1 MB default
	}

	if c.CleanupTimeout <= 0 {
		c.CleanupTimeout = 30 * time.Second
	}

	return nil
}

// NewRunner creates a new Podman-backed Runner.
func NewRunner(cfg Config) (Runner, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	eng, err := newPodmanEngine(cfg.SocketURI)
	if err != nil {
		return nil, err
	}
	return newRunnerWithEngine(eng, cfg), nil
}
