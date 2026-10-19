package actors

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/VictorMilhomem/freecompute/freechains"
	"github.com/VictorMilhomem/freecompute/internal/ipfs"
	"github.com/VictorMilhomem/freecompute/internal/sandbox"
	"golang.org/x/crypto/ssh"
)

// FreechainsAdapter adapts *freechains.Client to ChainAPI.
type FreechainsAdapter struct {
	client *freechains.Client
	alias  freechains.Alias
	key    freechains.PrivateKeyPath
}

// NewFreechainsAdapter constructs a ChainAPI backed by *freechains.Client.
func NewFreechainsAdapter(client *freechains.Client, chainAlias, signingKeyPath string) *FreechainsAdapter {
	return &FreechainsAdapter{
		client: client,
		alias:  freechains.Alias(chainAlias),
		key:    freechains.PrivateKeyPath(signingKeyPath),
	}
}

func (a *FreechainsAdapter) Post(ctx context.Context, payload []byte) (string, error) {
	tmp, err := os.CreateTemp("", "vc-post-*.bin")
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
		Sign: a.key,
	})
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func (a *FreechainsAdapter) ListOrder(ctx context.Context) ([]OrderItem, error) {
	entries, err := a.client.ListOrder(ctx, a.alias)
	if err != nil {
		return nil, err
	}
	out := make([]OrderItem, len(entries))
	for i, e := range entries {
		out[i] = OrderItem{
			ID:      e.ID.String(),
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
		signer = m.Signer.String()
	}
	return ActionMeta{
		ID:     m.ID.String(),
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
		Sign: a.key,
	})
	return err
}

func (a *FreechainsAdapter) DislikeAuthor(ctx context.Context, amount int64, authorPub string) error {
	_, err := a.client.Dislike(ctx, a.alias, amount, freechains.AuthorTarget(freechains.PublicKey(authorPub)), freechains.VoteOptions{
		Sign: a.key,
	})
	return err
}

func (a *FreechainsAdapter) SyncRecv(ctx context.Context, remote string) error {
	return a.client.SyncRecv(ctx, a.alias, remote)
}

func (a *FreechainsAdapter) SyncSend(ctx context.Context, remote string) error {
	return a.client.SyncSend(ctx, a.alias, remote)
}

// ReadPublicKey extracts the normalized "<type> <base64>" public key corresponding
// to a private key path, matching the format authenticated in Freechains metadata.
func ReadPublicKey(privateKeyPath string) (string, error) {
	if pubBytes, err := os.ReadFile(privateKeyPath + ".pub"); err == nil {
		fields := strings.Fields(string(pubBytes))
		if len(fields) >= 2 {
			return fields[0] + " " + fields[1], nil
		}
	}
	privBytes, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read key %s: %w", privateKeyPath, err)
	}
	signer, err := ssh.ParsePrivateKey(privBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse private key %s: %w", privateKeyPath, err)
	}
	fields := strings.Fields(string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	if len(fields) < 2 {
		return "", fmt.Errorf("unexpected public key format for %s", privateKeyPath)
	}
	return fields[0] + " " + fields[1], nil
}

// IPFSAdapter adapts ipfs.Store to the actors.Store port.
type IPFSAdapter struct {
	store  ipfs.Store
	limits ipfs.FetchLimits
}

// NewIPFSAdapter constructs a Store adapter around ipfs.Store.
func NewIPFSAdapter(store ipfs.Store, limits ipfs.FetchLimits) *IPFSAdapter {
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = 10 * 1024 * 1024
	}
	if limits.MaxFiles <= 0 {
		limits.MaxFiles = 100
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = 8
	}
	return &IPFSAdapter{store: store, limits: limits}
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
	cid, err := ipfs.ParseCID(cidStr)
	if err != nil {
		return err
	}
	if _, err := a.store.Fetch(ctx, cid, dest, a.limits); err != nil {
		return err
	}
	// internal/ipfs.Fetch materializes a single unnamed file node as "content".
	// Rename it to "data.txt" when fetching a single input file so the container
	// command can read /input/data.txt directly.
	contentPath := filepath.Join(dest, "content")
	dataPath := filepath.Join(dest, "data.txt")
	if info, err := os.Stat(contentPath); err == nil && info.Mode().IsRegular() {
		if _, err := os.Stat(dataPath); os.IsNotExist(err) {
			if err := os.Rename(contentPath, dataPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *IPFSAdapter) Ping(ctx context.Context) error {
	return a.store.Ping(ctx)
}

// SandboxAdapter adapts sandbox.Runner to the actors.Runner port.
type SandboxAdapter struct {
	runner sandbox.Runner
	limits sandbox.Limits
}

// NewSandboxAdapter constructs a Runner adapter around sandbox.Runner.
func NewSandboxAdapter(runner sandbox.Runner, limits sandbox.Limits) *SandboxAdapter {
	return &SandboxAdapter{runner: runner, limits: limits}
}

func (a *SandboxAdapter) Run(ctx context.Context, imageRef string, cmd []string, inputDir, outputDir string) (int32, error) {
	ref, err := sandbox.ParseImageRef(imageRef)
	if err != nil {
		return -1, err
	}
	relabelForSELinux(inputDir)
	relabelForSELinux(outputDir)
	res, err := a.runner.Run(ctx, sandbox.Spec{
		Image:     ref,
		Cmd:       cmd,
		InputDir:  inputDir,
		OutputDir: outputDir,
		Limits:    a.limits,
	})
	if err != nil {
		return res.ExitCode, err
	}
	if res.ExitCode != 0 {
		return res.ExitCode, fmt.Errorf("container exited with code %d: %s", res.ExitCode, strings.TrimSpace(string(res.Stderr)))
	}
	return res.ExitCode, nil
}

// relabelForSELinux sets container_file_t on bind-mounted directories so
// rootless Podman containers can read/write them on SELinux-enforcing hosts.
func relabelForSELinux(root string) {
	const label = "unconfined_u:object_r:container_file_t:s0\x00"
	_ = filepath.WalkDir(root, func(p string, _ os.DirEntry, err error) error {
		if err == nil {
			_ = syscall.Setxattr(p, "security.selinux", []byte(label), 0)
		}
		return nil
	})
}

func (a *SandboxAdapter) Cleanup(ctx context.Context) error {
	return a.runner.Cleanup(ctx)
}

func (a *SandboxAdapter) Ping(ctx context.Context) error {
	return a.runner.Ping(ctx)
}
