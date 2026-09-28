package freechains

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestHelperProcess provides the helper process for ExecRunner tests.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}
		args = args[1:]
	}

	if len(args) == 0 {
		os.Exit(0)
	}

	switch args[0] {
	case "success":
		fmt.Print("success stdout")
		os.Exit(0)
	case "failure":
		fmt.Fprint(os.Stderr, "failed with error")
		os.Exit(42)
	case "sleep":
		time.Sleep(5 * time.Second)
		os.Exit(0)
	default:
		os.Exit(1)
	}
}

func TestExecRunner(t *testing.T) {
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	runner := NewExecRunner()
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		cmd := Command{
			Args: []string{os.Args[0], "-test.run=TestHelperProcess", "--", "success"},
		}
		res, err := runner.Run(ctx, cmd)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 0 {
			t.Errorf("expected exit code 0, got %d", res.ExitCode)
		}
		if string(res.Stdout) != "success stdout" {
			t.Errorf("expected 'success stdout', got %q", string(res.Stdout))
		}
	})

	t.Run("non-zero exit with stderr", func(t *testing.T) {
		cmd := Command{
			Args: []string{os.Args[0], "-test.run=TestHelperProcess", "--", "failure"},
		}
		res, err := runner.Run(ctx, cmd)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.ExitCode != 42 {
			t.Errorf("expected exit code 42, got %d", res.ExitCode)
		}
		if string(res.Stderr) != "failed with error" {
			t.Errorf("expected 'failed with error', got %q", string(res.Stderr))
		}
	})

	t.Run("context timeout kills process", func(t *testing.T) {
		timeoutCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()

		cmd := Command{
			Args: []string{os.Args[0], "-test.run=TestHelperProcess", "--", "sleep"},
		}
		start := time.Now()
		_, err := runner.Run(timeoutCtx, cmd)
		elapsed := time.Since(start)

		if err == nil {
			t.Fatal("expected timeout error, got nil")
		}
		if !errors.Is(err, ErrCommandTimeout) {
			t.Errorf("expected error wrapping ErrCommandTimeout, got %v", err)
		}
		if elapsed > 3*time.Second {
			t.Errorf("process was not killed promptly, elapsed: %v", elapsed)
		}
	})
}

type concurrencyTracker struct {
	current int64
	max     int64
}

func (c *concurrencyTracker) Run(ctx context.Context, cmd Command) (Result, error) {
	cur := atomic.AddInt64(&c.current, 1)
	for {
		oldMax := atomic.LoadInt64(&c.max)
		if cur <= oldMax {
			break
		}
		if atomic.CompareAndSwapInt64(&c.max, oldMax, cur) {
			break
		}
	}

	time.Sleep(10 * time.Millisecond)
	atomic.AddInt64(&c.current, -1)
	return Result{ExitCode: 0}, nil
}

func TestSerialRunner(t *testing.T) {
	tracker := &concurrencyTracker{}
	serial := NewSerialRunner(tracker)

	const callers = 10
	var wg sync.WaitGroup
	wg.Add(callers)

	ctx := context.Background()
	cmd := Command{Args: []string{"test"}}

	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			_, _ = serial.Run(ctx, cmd)
		}()
	}

	wg.Wait()

	maxConcurrency := atomic.LoadInt64(&tracker.max)
	if maxConcurrency != 1 {
		t.Fatalf("expected max concurrency to be 1, got %d", maxConcurrency)
	}
}
