package freechains

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"
)

// Config configures the Freechains client.
type Config struct {
	Binary         string
	Root           string
	Now            int64
	DefaultKey     PrivateKeyPath
	CommandTimeout time.Duration
	Classifier     ErrorClassifier
}

// Client provides a typed interface over the Freechains CLI.
type Client struct {
	runner     Runner
	cfg        Config
	builder    *argsBuilder
	classifier ErrorClassifier
}

// NewClient validates the configuration and returns a new Client.
func NewClient(runner Runner, cfg Config) (*Client, error) {
	if runner == nil {
		return nil, fmt.Errorf("%w: runner cannot be nil", ErrInvalidArgument)
	}

	if cfg.Binary == "" {
		cfg.Binary = "freechains"
	}

	if cfg.CommandTimeout == 0 {
		cfg.CommandTimeout = 30 * time.Second
	} else if cfg.CommandTimeout < 0 {
		return nil, fmt.Errorf("%w: command timeout cannot be negative: %v", ErrInvalidArgument, cfg.CommandTimeout)
	}

	if cfg.Now < 0 {
		return nil, fmt.Errorf("%w: now cannot be negative: %d", ErrInvalidArgument, cfg.Now)
	}

	if cfg.Root != "" && cfg.Root[0] == '-' {
		return nil, fmt.Errorf("%w: root cannot start with '-': %q", ErrInvalidArgument, cfg.Root)
	}

	if cfg.DefaultKey != "" {
		if err := cfg.DefaultKey.Validate(); err != nil {
			return nil, fmt.Errorf("%w: invalid default key: %v", ErrInvalidArgument, err)
		}
	}

	classifier := cfg.Classifier
	if classifier == nil {
		classifier = DefaultErrorClassifier
	}

	builder := newArgsBuilder(cfg.Binary, cfg.Root, cfg.Now)

	return &Client{
		runner:     runner,
		cfg:        cfg,
		builder:    builder,
		classifier: classifier,
	}, nil
}

func (c *Client) run(ctx context.Context, args []string) (Result, error) {
	if c.cfg.CommandTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.cfg.CommandTimeout)
		defer cancel()
	}

	res, err := c.runner.Run(ctx, Command{Args: args})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || (ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
			return Result{}, fmt.Errorf("%w: %v", ErrCommandTimeout, err)
		}
		return Result{}, err
	}

	if res.ExitCode != 0 {
		classified := c.classifier(res.ExitCode, string(res.Stderr))
		if classified != nil {
			return Result{}, classified
		}
		return Result{}, &CommandError{
			Args:     args,
			ExitCode: res.ExitCode,
			Stderr:   string(res.Stderr),
		}
	}

	return res, nil
}

// ChainsDir lists all chains known to the local repository.
func (c *Client) ChainsDir(ctx context.Context) ([]Alias, error) {
	args := c.builder.chainsDir()
	res, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return ParseChainsDir(bytes.NewReader(res.Stdout))
}

// ChainsAddInit initializes a new chain and prints the resulting ChainID.
func (c *Client) ChainsAddInit(ctx context.Context, alias Alias, pioneers ...PublicKey) (ChainID, error) {
	args, err := c.builder.chainsAddInit(alias, pioneers...)
	if err != nil {
		return "", err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return ParseChainID(bytes.NewReader(res.Stdout))
}

// ChainsAddClone clones an existing chain from a remote URL.
func (c *Client) ChainsAddClone(ctx context.Context, alias Alias, url string) (ChainID, error) {
	args, err := c.builder.chainsAddClone(alias, url)
	if err != nil {
		return "", err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return ParseChainID(bytes.NewReader(res.Stdout))
}

// ChainsRem removes a chain and all associated local data.
func (c *Client) ChainsRem(ctx context.Context, alias Alias) error {
	args, err := c.builder.chainsRem(alias)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, args)
	return err
}

// Post publishes a new action (inline text or file) to the chain and returns its ActionID.
func (c *Client) Post(ctx context.Context, alias Alias, payload Payload, opts PostOptions) (ActionID, error) {
	if opts.Sign == "" && !opts.Beg && c.cfg.DefaultKey != "" {
		opts.Sign = c.cfg.DefaultKey
	}
	args, err := c.builder.post(alias, payload, opts)
	if err != nil {
		return "", err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return ParseActionID(bytes.NewReader(res.Stdout))
}

// Like spends reputation to upvote an action or author.
func (c *Client) Like(ctx context.Context, alias Alias, amount int64, target Target, opts VoteOptions) (ActionID, error) {
	if opts.Sign == "" && c.cfg.DefaultKey != "" {
		opts.Sign = c.cfg.DefaultKey
	}
	args, err := c.builder.like(alias, amount, target, opts)
	if err != nil {
		return "", err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return ParseActionID(bytes.NewReader(res.Stdout))
}

// Dislike spends reputation to downvote an action or author.
func (c *Client) Dislike(ctx context.Context, alias Alias, amount int64, target Target, opts VoteOptions) (ActionID, error) {
	if opts.Sign == "" && c.cfg.DefaultKey != "" {
		opts.Sign = c.cfg.DefaultKey
	}
	args, err := c.builder.dislike(alias, amount, target, opts)
	if err != nil {
		return "", err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return ParseActionID(bytes.NewReader(res.Stdout))
}

// Revoke revokes the payload of an action on the chain.
func (c *Client) Revoke(ctx context.Context, alias Alias, amount int64, id ActionID, opts VoteOptions) (ActionID, error) {
	if opts.Sign == "" && c.cfg.DefaultKey != "" {
		opts.Sign = c.cfg.DefaultKey
	}
	args, err := c.builder.revoke(alias, amount, id, opts)
	if err != nil {
		return "", err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return ParseActionID(bytes.NewReader(res.Stdout))
}

// Unrevoke restores a previously revoked payload.
func (c *Client) Unrevoke(ctx context.Context, alias Alias, amount int64, id ActionID, opts VoteOptions) (ActionID, error) {
	if opts.Sign == "" && c.cfg.DefaultKey != "" {
		opts.Sign = c.cfg.DefaultKey
	}
	args, err := c.builder.unrevoke(alias, amount, id, opts)
	if err != nil {
		return "", err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return ParseActionID(bytes.NewReader(res.Stdout))
}

// ListOrder returns the consensus order of actions in the chain.
func (c *Client) ListOrder(ctx context.Context, alias Alias) ([]OrderEntry, error) {
	args, err := c.builder.list(alias, "order")
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return ParseOrder(bytes.NewReader(res.Stdout))
}

// ListDag returns the ASCII representation of the DAG.
func (c *Client) ListDag(ctx context.Context, alias Alias) (string, error) {
	args, err := c.builder.list(alias, "dag")
	if err != nil {
		return "", err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return "", err
	}
	return string(res.Stdout), nil
}

// ListBegs returns pending begged action IDs awaiting reputation.
func (c *Client) ListBegs(ctx context.Context, alias Alias) ([]ActionID, error) {
	args, err := c.builder.list(alias, "begs")
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return ParseActionIDs(bytes.NewReader(res.Stdout))
}

// ListRevokes returns IDs of all revoked actions in the chain.
func (c *Client) ListRevokes(ctx context.Context, alias Alias) ([]ActionID, error) {
	args, err := c.builder.list(alias, "revokes")
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return ParseActionIDs(bytes.NewReader(res.Stdout))
}

// GetMetadata fetches and parses the key-value metadata for an action.
func (c *Client) GetMetadata(ctx context.Context, alias Alias, id ActionID) (Metadata, error) {
	args, err := c.builder.get(alias, "metadata", id)
	if err != nil {
		return Metadata{}, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return Metadata{}, err
	}
	return ParseMetadata(bytes.NewReader(res.Stdout))
}

// GetPayload fetches the raw payload bytes for an action.
func (c *Client) GetPayload(ctx context.Context, alias Alias, id ActionID) ([]byte, error) {
	args, err := c.builder.get(alias, "payload", id)
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return res.Stdout, nil
}

// RepsActions returns reputations of all actions.
func (c *Client) RepsActions(ctx context.Context, alias Alias) ([]ActionRep, error) {
	args, err := c.builder.repsCategory(alias, "actions")
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return ParseRepsActions(bytes.NewReader(res.Stdout))
}

// RepsAuthors returns reputations of all authors.
func (c *Client) RepsAuthors(ctx context.Context, alias Alias) ([]AuthorRep, error) {
	args, err := c.builder.repsCategory(alias, "authors")
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return ParseRepsAuthors(bytes.NewReader(res.Stdout))
}

// RepsRevokes returns revoke reputations for all revoked actions.
func (c *Client) RepsRevokes(ctx context.Context, alias Alias) ([]RevokeRep, error) {
	args, err := c.builder.repsCategory(alias, "revokes")
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return nil, err
	}
	return ParseRepsRevokes(bytes.NewReader(res.Stdout))
}

// RepsAction returns the reputation score for a single action.
func (c *Client) RepsAction(ctx context.Context, alias Alias, id ActionID) (int64, error) {
	args, err := c.builder.repsAction(alias, id)
	if err != nil {
		return 0, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return 0, err
	}
	return ParseInt64(bytes.NewReader(res.Stdout))
}

// RepsAuthor returns the reputation score for a single author.
func (c *Client) RepsAuthor(ctx context.Context, alias Alias, pub PublicKey) (int64, error) {
	args, err := c.builder.repsAuthor(alias, pub)
	if err != nil {
		return 0, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return 0, err
	}
	return ParseInt64(bytes.NewReader(res.Stdout))
}

// RepsRevoke returns the author and others revoke reputation for an action.
func (c *Client) RepsRevoke(ctx context.Context, alias Alias, id ActionID) (RevokeReps, error) {
	args, err := c.builder.repsRevoke(alias, id)
	if err != nil {
		return RevokeReps{}, err
	}
	res, err := c.run(ctx, args)
	if err != nil {
		return RevokeReps{}, err
	}
	return ParseRepsRevoke(bytes.NewReader(res.Stdout))
}

// SyncRecv synchronizes actions by receiving missing items from remote.
func (c *Client) SyncRecv(ctx context.Context, alias Alias, remote string) error {
	args, err := c.builder.sync(alias, "recv", remote)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, args)
	return err
}

// SyncSend synchronizes actions by sending missing items to remote.
func (c *Client) SyncSend(ctx context.Context, alias Alias, remote string) error {
	args, err := c.builder.sync(alias, "send", remote)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, args)
	return err
}

// Abandon drops actions after the specified action ID (or keeps up to ID with keep=true).
func (c *Client) Abandon(ctx context.Context, alias Alias, id ActionID, keep bool) error {
	args, err := c.builder.abandon(alias, id, keep)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, args)
	return err
}

// Sweep removes revoked payloads and compacts the chain repository.
func (c *Client) Sweep(ctx context.Context, alias Alias) error {
	args, err := c.builder.sweep(alias)
	if err != nil {
		return err
	}
	_, err = c.run(ctx, args)
	return err
}
