package freechains

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
)

// DaemonConfig holds configuration options for starting and managing the Freechains daemon.
type DaemonConfig struct {
	Binary  string
	Root    string
	Now     int64
	Port    int
	Hub     bool
	GitOpts []string
}

// Daemon manages the lifecycle of a long-running Freechains daemon process.
type Daemon struct {
	cfg     DaemonConfig
	runner  Runner
	builder *argsBuilder

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}
	err    error
	stderr *bytes.Buffer
}

// NewDaemon initializes a new Daemon instance.
func NewDaemon(runner Runner, cfg DaemonConfig) (*Daemon, error) {
	if runner == nil {
		return nil, fmt.Errorf("%w: runner cannot be nil", ErrInvalidArgument)
	}

	if cfg.Binary == "" {
		cfg.Binary = "freechains"
	}

	if cfg.Port < 0 {
		return nil, fmt.Errorf("%w: port cannot be negative: %d", ErrInvalidArgument, cfg.Port)
	}

	if cfg.Root != "" && cfg.Root[0] == '-' {
		return nil, fmt.Errorf("%w: root cannot start with '-': %q", ErrInvalidArgument, cfg.Root)
	}

	builder := newArgsBuilder(cfg.Binary, cfg.Root, cfg.Now)

	return &Daemon{
		cfg:     cfg,
		runner:  runner,
		builder: builder,
	}, nil
}

// Start launches the daemon process and returns once it is actively running and ready.
// It does not block on daemon execution.
func (d *Daemon) Start(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cmd != nil && d.cmd.Process != nil {
		return fmt.Errorf("%w: daemon already started", ErrInvalidArgument)
	}

	daemonCtx, daemonCancel := context.WithCancel(context.Background())
	args := d.builder.daemonStart(d.cfg.Port, d.cfg.Hub, d.cfg.GitOpts)

	cmd := exec.CommandContext(daemonCtx, args[0], args[1:]...)
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		daemonCancel()
		return fmt.Errorf("daemon stdout pipe failed: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	d.stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		daemonCancel()
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	d.cmd = cmd
	d.cancel = daemonCancel
	d.done = make(chan struct{})

	go func() {
		d.err = cmd.Wait()
		close(d.done)
	}()

	ready := make(chan struct{})
	readErr := make(chan error, 1)

	go func() {
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if strings.Contains(line, "Serving on port") {
				close(ready)
				return
			}
			if err != nil {
				if !errors.Is(err, io.EOF) {
					readErr <- err
				}
				return
			}
		}
	}()

	select {
	case <-ready:
		return nil
	case err := <-readErr:
		d.cancel()
		<-d.done
		return fmt.Errorf("daemon initialization failed reading output: %w", err)
	case <-d.done:
		d.cancel()
		stderrText := strings.TrimSpace(d.stderr.String())
		if stderrText != "" {
			return fmt.Errorf("daemon exited prematurely: %s", stderrText)
		}
		if d.err != nil {
			return fmt.Errorf("daemon exited prematurely: %w", d.err)
		}
		return errors.New("daemon exited unexpectedly during startup")
	case <-ctx.Done():
		d.cancel()
		<-d.done
		return ctx.Err()
	}
}

// Stop issues a "daemon stop" command to cleanly stop the daemon, waiting for exit.
func (d *Daemon) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cmd == nil || d.cmd.Process == nil {
		return nil
	}

	stopArgs := d.builder.daemonStop()
	_, stopErr := d.runner.Run(ctx, Command{Args: stopArgs})

	select {
	case <-d.done:
		d.cancel()
		d.cmd = nil
		return stopErr
	case <-ctx.Done():
		d.cancel()
		<-d.done
		d.cmd = nil
		return ctx.Err()
	}
}
