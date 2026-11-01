package actors

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/VictorMilhomem/freecompute/freechains"
	"github.com/VictorMilhomem/freecompute/internal/ipfs"
	"github.com/VictorMilhomem/freecompute/internal/sandbox"
	"golang.org/x/sys/unix"
)

func ReadPublicKey(privKeyPath string) (string, error) {
	pubPath := privKeyPath
	if !strings.HasSuffix(pubPath, ".pub") {
		pubPath += ".pub"
	}
	data, err := os.ReadFile(pubPath)
	if err != nil {
		return "", fmt.Errorf("failed to read public key %q: %w", pubPath, err)
	}
	fields := strings.Fields(strings.TrimSpace(string(data)))
	if len(fields) < 2 {
		return "", fmt.Errorf("invalid public key format in %q", pubPath)
	}
	return fields[0] + " " + fields[1], nil
}

type FreechainsAdapter struct {
	client  *freechains.Client
	alias   freechains.Alias
	keyPath freechains.PrivateKeyPath
}

func NewFreechainsAdapter(client *freechains.Client, chainAlias string, keyPath string) *FreechainsAdapter {
	return &FreechainsAdapter{
		client:  client,
		alias:   freechains.Alias(chainAlias),
		keyPath: freechains.PrivateKeyPath(keyPath),
	}
}

func (a *FreechainsAdapter) Post(ctx context.Context, payload []byte) (string, error) {
	tmp, err := os.CreateTemp("", "vc-post-*.pb")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}

	id, err := a.client.Post(ctx, a.alias, freechains.FilePayload(tmpPath), freechains.PostOptions{
		Sign: a.keyPath,
	})
	if err != nil {
		return "", err
	}
	return string(id), nil
}

func (a *FreechainsAdapter) ListOrder(ctx context.Context) ([]OrderItem, error) {
	entries, err := a.client.ListOrder(ctx, a.alias)
	if err != nil {
		return nil, err
	}
	out := make([]OrderItem, len(entries))
	for i, e := range entries {
		out[i] = OrderItem{
			ID:      string(e.ID),
			Revoked: e.Revoked,
		}
	}
	return out, nil
}

func (a *FreechainsAdapter) GetMetadata(ctx context.Context, id string) (ActionMeta, error) {
	m, err := a.client.GetMetadata(ctx, a.alias, freechains.ActionID(id))
	if err != nil {
		return ActionMeta{}, err
	}
	var signer string
	if m.Signer != nil {
		signer = string(*m.Signer)
	}
	return ActionMeta{
		ID:     string(m.ID),
		Kind:   string(m.Kind),
		Signer: signer,
		Time:   m.Time,
	}, nil
}

func (a *FreechainsAdapter) GetPayload(ctx context.Context, id string) ([]byte, error) {
	return a.client.GetPayload(ctx, a.alias, freechains.ActionID(id))
}

func (a *FreechainsAdapter) LikeAuthor(ctx context.Context, amount int64, authorPub string) error {
	_, err := a.client.Like(ctx, a.alias, amount, freechains.AuthorTarget(freechains.PublicKey(authorPub)), freechains.VoteOptions{
		Sign: a.keyPath,
	})
	return err
}

func (a *FreechainsAdapter) DislikeAuthor(ctx context.Context, amount int64, authorPub string) error {
	_, err := a.client.Dislike(ctx, a.alias, amount, freechains.AuthorTarget(freechains.PublicKey(authorPub)), freechains.VoteOptions{
		Sign: a.keyPath,
	})
	return err
}

func (a *FreechainsAdapter) RepsAuthor(ctx context.Context, authorPub string) (int64, error) {
	return a.client.RepsAuthor(ctx, a.alias, freechains.PublicKey(authorPub))
}

func (a *FreechainsAdapter) SyncRecv(ctx context.Context, remote string) error {
	return a.client.SyncRecv(ctx, a.alias, remote)
}

func (a *FreechainsAdapter) SyncSend(ctx context.Context, remote string) error {
	return a.client.SyncSend(ctx, a.alias, remote)
}

type IPFSAdapter struct {
	store  ipfs.Store
	limits ipfs.FetchLimits
}

func NewIPFSAdapter(store ipfs.Store, limits ipfs.FetchLimits) *IPFSAdapter {
	return &IPFSAdapter{
		store:  store,
		limits: limits,
	}
}

func (a *IPFSAdapter) Add(ctx context.Context, r io.Reader) (string, error) {
	cid, err := a.store.Add(ctx, r)
	if err != nil {
		return "", err
	}
	return cid.String(), nil
}

func (a *IPFSAdapter) AddDir(ctx context.Context, dir string) (string, error) {
	cid, err := a.store.AddDir(ctx, dir)
	if err != nil {
		return "", err
	}
	return cid.String(), nil
}

func (a *IPFSAdapter) Fetch(ctx context.Context, cidStr string, dest string) error {
	id, err := ipfs.ParseCID(cidStr)
	if err != nil {
		return err
	}
	_, err = a.store.Fetch(ctx, id, dest, a.limits)
	return err
}

func (a *IPFSAdapter) Ping(ctx context.Context) error {
	return a.store.Ping(ctx)
}

type SandboxAdapter struct {
	runner sandbox.Runner
	limits sandbox.Limits
	policy sandbox.ImagePolicy
}

func NewSandboxAdapter(runner sandbox.Runner, limits sandbox.Limits, policy ...sandbox.ImagePolicy) *SandboxAdapter {
	var p sandbox.ImagePolicy
	if len(policy) > 0 {
		p = policy[0]
	}
	return &SandboxAdapter{
		runner: runner,
		limits: limits,
		policy: p,
	}
}

func (a *SandboxAdapter) Run(ctx context.Context, imageRef string, cmd []string, inputDir, outputDir string) (int32, error) {
	ref, err := sandbox.ParseImageRef(imageRef)
	if err != nil {
		return -1, err
	}
	if a.policy != nil {
		if err := a.policy.Allow(ref); err != nil {
			return -1, fmt.Errorf("%w: %v", sandbox.ErrImageNotAllowed, err)
		}
	}
	if err := a.runner.EnsureImage(ctx, ref); err != nil {
		return -1, err
	}

	_ = filepath.WalkDir(inputDir, func(p string, _ os.DirEntry, err error) error {
		if err == nil {
			labelForContainer(p)
		}
		return nil
	})
	labelForContainer(outputDir)

	res, err := a.runner.Run(ctx, sandbox.Spec{
		Image:     ref,
		Cmd:       cmd,
		InputDir:  inputDir,
		OutputDir: outputDir,
		Limits:    a.limits,
	})
	if err != nil {
		return -1, err
	}
	return res.ExitCode, nil
}

// Rootless Podman on SELinux-enforcing hosts blocks container access to bind mounts
// in /tmp unless labeled container_file_t.
func labelForContainer(path string) {
	_ = unix.Setxattr(path, "security.selinux", []byte("unconfined_u:object_r:container_file_t:s0\x00"), 0)
}

func (a *SandboxAdapter) Cleanup(ctx context.Context) error {
	return a.runner.Cleanup(ctx)
}

func (a *SandboxAdapter) Ping(ctx context.Context) error {
	return a.runner.Ping(ctx)
}
