//go:build integration
// +build integration

package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegrationPodman(t *testing.T) {
	imageEnv := os.Getenv("SANDBOX_TEST_IMAGE")
	if imageEnv == "" {
		t.Skip("skipping sandbox integration test: SANDBOX_TEST_IMAGE not set")
	}

	imageRef, err := ParseImageRef(imageEnv)
	if err != nil {
		t.Fatalf("invalid SANDBOX_TEST_IMAGE: %v", err)
	}

	cfg := Config{
		RequireRootless: true,
		MaxConcurrent:   1,
		DefaultLimits: Limits{
			CPUs:           1.0,
			MemoryBytes:    256 * 1024 * 1024,
			Timeout:        60 * time.Second,
			PidsLimit:      100,
			TmpfsBytes:     64 * 1024 * 1024,
			MaxOutputBytes: 10 * 1024 * 1024,
			MaxOutputFiles: 100,
		},
		HardLimits: Limits{
			CPUs:           2.0,
			MemoryBytes:    1024 * 1024 * 1024,
			Timeout:        120 * time.Second,
			PidsLimit:      200,
			TmpfsBytes:     128 * 1024 * 1024,
			MaxOutputBytes: 50 * 1024 * 1024,
			MaxOutputFiles: 500,
		},
	}

	runner, err := NewRunner(cfg)
	if err != nil {
		t.Fatalf("failed to create runner: %v", err)
	}

	ctx := context.Background()
	if err := runner.Ping(ctx); err != nil {
		t.Skipf("skipping sandbox integration test: podman service unavailable: %v", err)
	}

	inDir := t.TempDir()
	outDir := t.TempDir()

	_ = os.WriteFile(filepath.Join(inDir, "input.txt"), []byte("data"), 0644)

	spec := Spec{
		Image:     imageRef,
		Cmd:       []string{"sh", "-c", "cat /input/input.txt > /output/result.txt && echo finished"},
		InputDir:  inDir,
		OutputDir: outDir,
		Limits:    cfg.DefaultLimits,
	}

	res, err := runner.Run(ctx, spec)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d (stderr: %s)", res.ExitCode, string(res.Stderr))
	}

	resultBytes, err := os.ReadFile(filepath.Join(outDir, "result.txt"))
	if err != nil {
		t.Fatalf("failed to read result.txt: %v", err)
	}
	if string(resultBytes) != "data" {
		t.Errorf("result content mismatch: %q", string(resultBytes))
	}
}
