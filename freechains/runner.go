package freechains

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

// Command holds the arguments and optional standard input for a runner invocation.
type Command struct {
	Args  []string
	Stdin io.Reader
}

// Result holds the captured outputs and process exit code from a runner invocation.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// Runner abstracts process execution, enabling testability and decoration.
type Runner interface {
	Run(ctx context.Context, cmd Command) (Result, error)
}

// ExecRunner executes commands directly via the operating system using os/exec.
type ExecRunner struct{}

// NewExecRunner creates a new ExecRunner.
func NewExecRunner() *ExecRunner {
	return &ExecRunner{}
}

// Run executes the command without a shell, honoring context cancellation and timeouts.
func (e *ExecRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	if len(cmd.Args) == 0 {
		return Result{}, fmt.Errorf("%w: command args cannot be empty", ErrInvalidArgument)
	}

	execCmd := exec.CommandContext(ctx, cmd.Args[0], cmd.Args[1:]...)
	execCmd.Cancel = func() error {
		if execCmd.Process != nil {
			return execCmd.Process.Kill()
		}
		return nil
	}

	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	if cmd.Stdin != nil {
		execCmd.Stdin = cmd.Stdin
	}

	err := execCmd.Run()
	res := Result{
		Stdout: stdout.Bytes(),
		Stderr: stderr.Bytes(),
	}

	if err != nil {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return res, fmt.Errorf("%w: %v", ErrCommandTimeout, ctx.Err())
			}
			return res, ctx.Err()
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
			return res, nil
		}
		return res, err
	}

	return res, nil
}

// SerialRunner decorates any Runner with a mutex to ensure serialized execution.
type SerialRunner struct {
	mu     sync.Mutex
	runner Runner
}

// NewSerialRunner wraps an existing Runner with execution serialization.
func NewSerialRunner(runner Runner) *SerialRunner {
	return &SerialRunner{runner: runner}
}

// Run acquires the mutex before delegating to the wrapped Runner.
func (s *SerialRunner) Run(ctx context.Context, cmd Command) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runner.Run(ctx, cmd)
}
