package ipfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"
)

// Store defines the public interface for content-addressed storage operations.
type Store interface {
	Add(ctx context.Context, r io.Reader) (CID, error)
	AddDir(ctx context.Context, dir string) (CID, error)
	Cat(ctx context.Context, id CID, maxBytes int64) (io.ReadCloser, error)
	Fetch(ctx context.Context, id CID, dest string, lim FetchLimits) (FetchResult, error)
	Stat(ctx context.Context, id CID) (Stat, error)
	Pin(ctx context.Context, id CID) error
	Unpin(ctx context.Context, id CID) error
	IsPinned(ctx context.Context, id CID) (bool, error)
	Ping(ctx context.Context) error
}

// Stat describes content-addressed object metadata.
type Stat struct {
	Size     uint64
	IsDir    bool
	NumLinks int
}

// FetchLimits bounds disk materialization resource usage.
type FetchLimits struct {
	MaxBytes int64
	MaxFiles int
	MaxDepth int
}

// FetchResult summarizes the outcome of a Store.Fetch call.
type FetchResult struct {
	Bytes int64
	Files int
}

// Config configures the IPFS Store client.
type Config struct {
	APIAddr            string
	RequestTimeout     time.Duration
	AllowRemote        bool
	DefaultFetchLimits FetchLimits
}

// Validate checks and applies configuration defaults.
func (c *Config) Validate() error {
	if c.APIAddr == "" {
		c.APIAddr = "http://127.0.0.1:5001"
	}
	if c.RequestTimeout == 0 {
		c.RequestTimeout = 60 * time.Second
	} else if c.RequestTimeout < 0 {
		return fmt.Errorf("%w: request timeout cannot be negative", ErrInvalidArgument)
	}

	if !c.AllowRemote {
		if err := validateLocalAddress(c.APIAddr); err != nil {
			return err
		}
	}

	return nil
}

func validateLocalAddress(addr string) error {
	// Unix sockets are always local
	if strings.HasPrefix(addr, "unix://") || strings.HasPrefix(addr, "/unix/") || strings.HasPrefix(addr, "/") {
		return nil
	}

	raw := addr
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: invalid API address %q: %v", ErrInvalidArgument, addr, err)
	}

	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}

	return fmt.Errorf("%w: non-local API address %q not permitted", ErrInvalidArgument, addr)
}

// ContentNodeType designates the category of an IPFS node.
type ContentNodeType int

const (
	NodeFile ContentNodeType = iota
	NodeDir
	NodeSymlink
	NodeOther
)

// ContentNode abstracts a file or directory tree retrieved from IPFS.
type ContentNode interface {
	Type() ContentNodeType
	Name() string
	Size() int64
	Open() (io.ReadCloser, error)
	Entries(ctx context.Context) ([]ContentNode, error)
}

// rpcAPI defines the narrow internal port implemented by the official Kubo adapter.
type rpcAPI interface {
	Add(ctx context.Context, r io.Reader) (CID, error)
	AddDir(ctx context.Context, dir string) (CID, error)
	Get(ctx context.Context, id CID) (ContentNode, error)
	Stat(ctx context.Context, id CID) (Stat, error)
	Pin(ctx context.Context, id CID) error
	Unpin(ctx context.Context, id CID) error
	IsPinned(ctx context.Context, id CID) (bool, error)
	Ping(ctx context.Context) error
}

type ipfsStore struct {
	api rpcAPI
	cfg Config
}

// NewStore initializes an IPFS Store connected to the Kubo RPC endpoint.
func NewStore(cfg Config) (Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	adapter, err := newKuboAdapter(cfg.APIAddr)
	if err != nil {
		return nil, err
	}
	return &ipfsStore{
		api: adapter,
		cfg: cfg,
	}, nil
}

func (s *ipfsStore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if s.cfg.RequestTimeout > 0 {
		return context.WithTimeout(ctx, s.cfg.RequestTimeout)
	}
	return context.WithCancel(ctx)
}

func mapTimeout(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	return err
}

func (s *ipfsStore) Add(ctx context.Context, r io.Reader) (CID, error) {
	if r == nil {
		return "", fmt.Errorf("%w: reader cannot be nil", ErrInvalidArgument)
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	cid, err := s.api.Add(ctx, r)
	if err != nil {
		return "", mapTimeout(err)
	}
	return cid, nil
}

func (s *ipfsStore) AddDir(ctx context.Context, dir string) (CID, error) {
	if dir == "" {
		return "", fmt.Errorf("%w: directory path cannot be empty", ErrInvalidArgument)
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	cid, err := s.api.AddDir(ctx, dir)
	if err != nil {
		return "", mapTimeout(err)
	}
	return cid, nil
}

func (s *ipfsStore) Cat(ctx context.Context, id CID, maxBytes int64) (io.ReadCloser, error) {
	if !id.Defined() {
		return nil, ErrInvalidCID
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	node, err := s.api.Get(ctx, id)
	if err != nil {
		return nil, mapTimeout(err)
	}
	if node.Type() != NodeFile {
		return nil, fmt.Errorf("%w: cannot cat non-file node", ErrInvalidArgument)
	}

	rc, err := node.Open()
	if err != nil {
		return nil, mapTimeout(err)
	}
	return newLimitedReader(rc, maxBytes), nil
}

func (s *ipfsStore) Fetch(ctx context.Context, id CID, dest string, lim FetchLimits) (FetchResult, error) {
	if !id.Defined() {
		return FetchResult{}, ErrInvalidCID
	}

	if lim.MaxBytes == 0 && s.cfg.DefaultFetchLimits.MaxBytes > 0 {
		lim.MaxBytes = s.cfg.DefaultFetchLimits.MaxBytes
	}
	if lim.MaxFiles == 0 && s.cfg.DefaultFetchLimits.MaxFiles > 0 {
		lim.MaxFiles = s.cfg.DefaultFetchLimits.MaxFiles
	}
	if lim.MaxDepth == 0 && s.cfg.DefaultFetchLimits.MaxDepth > 0 {
		lim.MaxDepth = s.cfg.DefaultFetchLimits.MaxDepth
	}

	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	// Pre-flight check via Stat when possible
	st, err := s.api.Stat(ctx, id)
	if err == nil {
		if lim.MaxBytes > 0 && int64(st.Size) > lim.MaxBytes {
			return FetchResult{}, fmt.Errorf("%w: stat size %d exceeds limit %d", ErrTooLarge, st.Size, lim.MaxBytes)
		}
		if lim.MaxFiles > 0 && st.NumLinks > lim.MaxFiles {
			return FetchResult{}, fmt.Errorf("%w: stat links %d exceeds limit %d", ErrTooLarge, st.NumLinks, lim.MaxFiles)
		}
	}

	node, err := s.api.Get(ctx, id)
	if err != nil {
		return FetchResult{}, mapTimeout(err)
	}

	res, err := safeMaterialize(ctx, node, dest, lim)
	if err != nil {
		return FetchResult{}, mapTimeout(err)
	}
	return res, nil
}

func (s *ipfsStore) Stat(ctx context.Context, id CID) (Stat, error) {
	if !id.Defined() {
		return Stat{}, ErrInvalidCID
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	st, err := s.api.Stat(ctx, id)
	if err != nil {
		return Stat{}, mapTimeout(err)
	}
	return st, nil
}

func (s *ipfsStore) Pin(ctx context.Context, id CID) error {
	if !id.Defined() {
		return ErrInvalidCID
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	return mapTimeout(s.api.Pin(ctx, id))
}

func (s *ipfsStore) Unpin(ctx context.Context, id CID) error {
	if !id.Defined() {
		return ErrInvalidCID
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	return mapTimeout(s.api.Unpin(ctx, id))
}

func (s *ipfsStore) IsPinned(ctx context.Context, id CID) (bool, error) {
	if !id.Defined() {
		return false, ErrInvalidCID
	}
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	pinned, err := s.api.IsPinned(ctx, id)
	if err != nil {
		return false, mapTimeout(err)
	}
	return pinned, nil
}

func (s *ipfsStore) Ping(ctx context.Context) error {
	ctx, cancel := s.withTimeout(ctx)
	defer cancel()

	return mapTimeout(s.api.Ping(ctx))
}
