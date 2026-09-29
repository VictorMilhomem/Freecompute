package sandbox

import (
	"context"
	"fmt"
	"sync"

	"github.com/containers/podman/v5/pkg/bindings"
	"github.com/containers/podman/v5/pkg/bindings/containers"
	"github.com/containers/podman/v5/pkg/bindings/images"
	"github.com/containers/podman/v5/pkg/bindings/system"
	"github.com/containers/podman/v5/pkg/specgen"
)

type podmanEngine struct {
	socketURI string
}

func newPodmanEngine(socketURI string) (engine, error) {
	if _, err := bindings.NewConnection(context.Background(), socketURI); err != nil {
		return nil, fmt.Errorf("%w: invalid socket URI %q: %v", ErrInvalidArgument, socketURI, err)
	}
	return &podmanEngine{socketURI: socketURI}, nil
}

func (p *podmanEngine) conn(ctx context.Context) (context.Context, error) {
	connCtx, err := bindings.NewConnection(ctx, p.socketURI)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to connect to podman socket: %v", ErrEngineUnavailable, err)
	}
	return connCtx, nil
}

func (p *podmanEngine) Info(ctx context.Context) (EngineInfo, error) {
	c, err := p.conn(ctx)
	if err != nil {
		return EngineInfo{}, err
	}
	info, err := system.Info(c, nil)
	if err != nil {
		return EngineInfo{}, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	rootless := false
	if info != nil && info.Host != nil {
		rootless = info.Host.Security.Rootless
	}
	return EngineInfo{Rootless: rootless}, nil
}

func (p *podmanEngine) ImageExists(ctx context.Context, ref string) (bool, error) {
	c, err := p.conn(ctx)
	if err != nil {
		return false, err
	}
	exists, err := images.Exists(c, ref, nil)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	return exists, nil
}

func (p *podmanEngine) PullImage(ctx context.Context, ref string) error {
	c, err := p.conn(ctx)
	if err != nil {
		return err
	}
	_, err = images.Pull(c, ref, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	return nil
}

func (p *podmanEngine) CreateContainer(ctx context.Context, s *specgen.SpecGenerator) (string, error) {
	c, err := p.conn(ctx)
	if err != nil {
		return "", err
	}
	resp, err := containers.CreateWithSpec(c, s, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	return resp.ID, nil
}

func (p *podmanEngine) Start(ctx context.Context, id string) error {
	c, err := p.conn(ctx)
	if err != nil {
		return err
	}
	if err := containers.Start(c, id, nil); err != nil {
		return fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	return nil
}

func (p *podmanEngine) Wait(ctx context.Context, id string) (int32, error) {
	c, err := p.conn(ctx)
	if err != nil {
		return -1, err
	}
	code, err := containers.Wait(c, id, nil)
	if err != nil {
		return -1, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	return code, nil
}

func (p *podmanEngine) Kill(ctx context.Context, id string, signal string) error {
	c, err := p.conn(ctx)
	if err != nil {
		return err
	}
	sig := signal
	if sig == "" {
		sig = "SIGKILL"
	}
	if err := containers.Kill(c, id, new(containers.KillOptions).WithSignal(sig)); err != nil {
		return fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	return nil
}

func (p *podmanEngine) Logs(ctx context.Context, id string, maxBytes int64) (stdout, stderr []byte, truncated bool, err error) {
	c, err := p.conn(ctx)
	if err != nil {
		return nil, nil, false, err
	}

	stdoutBuf := newBoundedLogBuffer(maxBytes)
	stderrBuf := newBoundedLogBuffer(maxBytes)

	stdoutChan := make(chan string)
	stderrChan := make(chan string)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for line := range stdoutChan {
			stdoutBuf.WriteString(line)
		}
	}()

	go func() {
		defer wg.Done()
		for line := range stderrChan {
			stderrBuf.WriteString(line)
		}
	}()

	opts := new(containers.LogOptions).WithStdout(true).WithStderr(true)
	logErr := containers.Logs(c, id, opts, stdoutChan, stderrChan)
	wg.Wait()

	truncated = stdoutBuf.Truncated() || stderrBuf.Truncated()
	if logErr != nil {
		return stdoutBuf.Bytes(), stderrBuf.Bytes(), truncated, fmt.Errorf("%w: %v", ErrEngineUnavailable, logErr)
	}

	return stdoutBuf.Bytes(), stderrBuf.Bytes(), truncated, nil
}

func (p *podmanEngine) Inspect(ctx context.Context, id string) (ContainerState, error) {
	c, err := p.conn(ctx)
	if err != nil {
		return ContainerState{}, err
	}
	data, err := containers.Inspect(c, id, nil)
	if err != nil {
		return ContainerState{}, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	if data == nil || data.State == nil {
		return ContainerState{}, nil
	}
	return ContainerState{
		ExitCode:  int32(data.State.ExitCode),
		OOMKilled: data.State.OOMKilled,
		Running:   data.State.Running,
	}, nil
}

func (p *podmanEngine) Remove(ctx context.Context, id string, force, volumes bool) error {
	c, err := p.conn(ctx)
	if err != nil {
		return err
	}
	opts := new(containers.RemoveOptions).WithForce(force).WithVolumes(volumes)
	if _, err := containers.Remove(c, id, opts); err != nil {
		return fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	return nil
}

func (p *podmanEngine) ListByLabel(ctx context.Context, label string) ([]string, error) {
	c, err := p.conn(ctx)
	if err != nil {
		return nil, err
	}
	opts := new(containers.ListOptions).WithFilters(map[string][]string{"label": {label}}).WithAll(true)
	list, err := containers.List(c, opts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEngineUnavailable, err)
	}
	var ids []string
	for _, item := range list {
		ids = append(ids, item.ID)
	}
	return ids, nil
}
