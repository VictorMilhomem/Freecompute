package ipfs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	files "github.com/ipfs/boxo/files"
	path "github.com/ipfs/boxo/path"
	gocid "github.com/ipfs/go-cid"
	rpc "github.com/ipfs/kubo/client/rpc"
	options "github.com/ipfs/kubo/core/coreiface/options"
	ma "github.com/multiformats/go-multiaddr"
)

type kuboAdapter struct {
	client *rpc.HttpApi
}

func newKuboAdapter(addr string) (rpcAPI, error) {
	var client *rpc.HttpApi
	var err error

	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		client, err = rpc.NewURLApiWithClient(addr, http.DefaultClient)
	} else {
		maddr, mErr := ma.NewMultiaddr(addr)
		if mErr != nil {
			return nil, fmt.Errorf("%w: invalid multiaddr %q: %v", ErrInvalidArgument, addr, mErr)
		}
		client, err = rpc.NewApiWithClient(maddr, http.DefaultClient)
	}

	if err != nil {
		return nil, fmt.Errorf("%w: failed to connect to Kubo API: %v", ErrDaemonUnavailable, err)
	}

	return &kuboAdapter{client: client}, nil
}

func (k *kuboAdapter) Add(ctx context.Context, r io.Reader) (CID, error) {
	node := files.NewReaderFile(r)
	p, err := k.client.Unixfs().Add(ctx, node,
		options.Unixfs.CidVersion(1),
		options.Unixfs.RawLeaves(true),
		options.Unixfs.Pin(true, ""),
		options.Unixfs.Chunker("size-262144"),
	)
	if err != nil {
		return "", mapKuboError(err)
	}
	return ParseCID(p.RootCid().String())
}

func (k *kuboAdapter) AddDir(ctx context.Context, dir string) (CID, error) {
	stat, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidArgument, err)
	}
	node, err := files.NewSerialFile(dir, false, stat)
	if err != nil {
		return "", fmt.Errorf("failed to open directory for adding: %w", err)
	}
	p, err := k.client.Unixfs().Add(ctx, node,
		options.Unixfs.CidVersion(1),
		options.Unixfs.RawLeaves(true),
		options.Unixfs.Pin(true, ""),
		options.Unixfs.Chunker("size-262144"),
	)
	if err != nil {
		return "", mapKuboError(err)
	}
	return ParseCID(p.RootCid().String())
}

func (k *kuboAdapter) Get(ctx context.Context, id CID) (ContentNode, error) {
	c, err := gocid.Decode(id.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCID, err)
	}
	node, err := k.client.Unixfs().Get(ctx, path.FromCid(c))
	if err != nil {
		return nil, mapKuboError(err)
	}
	return newKuboNode("", node), nil
}

type kuboNode struct {
	name string
	node files.Node
}

func newKuboNode(name string, node files.Node) *kuboNode {
	return &kuboNode{
		name: name,
		node: node,
	}
}

func (kn *kuboNode) Type() ContentNodeType {
	if files.ToSymlink(kn.node) != nil {
		return NodeSymlink
	}
	if files.ToDir(kn.node) != nil {
		return NodeDir
	}
	if files.ToFile(kn.node) != nil {
		return NodeFile
	}
	return NodeOther
}

func (kn *kuboNode) Name() string {
	return kn.name
}

func (kn *kuboNode) Size() int64 {
	size, err := kn.node.Size()
	if err != nil {
		return 0
	}
	return size
}

func (kn *kuboNode) Open() (io.ReadCloser, error) {
	f := files.ToFile(kn.node)
	if f == nil {
		return nil, fmt.Errorf("%w: node is not a file", ErrInvalidArgument)
	}
	return f, nil
}

func (kn *kuboNode) Entries(ctx context.Context) ([]ContentNode, error) {
	dir := files.ToDir(kn.node)
	if dir == nil {
		return nil, fmt.Errorf("%w: node is not a directory", ErrInvalidArgument)
	}
	it := dir.Entries()
	var entries []ContentNode
	for it.Next() {
		entries = append(entries, newKuboNode(it.Name(), it.Node()))
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

type kuboFilesStatResp struct {
	Hash           string `json:"Hash"`
	Size           uint64 `json:"Size"`
	CumulativeSize uint64 `json:"CumulativeSize"`
	Blocks         int    `json:"Blocks"`
	Type           string `json:"Type"`
}

func (k *kuboAdapter) Stat(ctx context.Context, id CID) (Stat, error) {
	req := k.client.Request("files/stat", "/ipfs/"+id.String())
	var resp kuboFilesStatResp
	if err := req.Exec(ctx, &resp); err != nil {
		return Stat{}, mapKuboError(err)
	}

	size := resp.Size
	if resp.Type == "directory" && resp.CumulativeSize > 0 {
		size = resp.CumulativeSize
	}

	return Stat{
		Size:     size,
		IsDir:    resp.Type == "directory",
		NumLinks: resp.Blocks,
	}, nil
}

func (k *kuboAdapter) Pin(ctx context.Context, id CID) error {
	c, err := gocid.Decode(id.String())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCID, err)
	}
	if err := k.client.Pin().Add(ctx, path.FromCid(c)); err != nil {
		return mapKuboError(err)
	}
	return nil
}

func (k *kuboAdapter) Unpin(ctx context.Context, id CID) error {
	c, err := gocid.Decode(id.String())
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCID, err)
	}
	if err := k.client.Pin().Rm(ctx, path.FromCid(c)); err != nil {
		return mapKuboError(err)
	}
	return nil
}

func (k *kuboAdapter) IsPinned(ctx context.Context, id CID) (bool, error) {
	c, err := gocid.Decode(id.String())
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidCID, err)
	}
	_, pinned, err := k.client.Pin().IsPinned(ctx, path.FromCid(c))
	if err != nil {
		return false, mapKuboError(err)
	}
	return pinned, nil
}

func (k *kuboAdapter) Ping(ctx context.Context) error {
	req := k.client.Request("id")
	if err := req.Exec(ctx, nil); err != nil {
		return fmt.Errorf("%w: %v", ErrDaemonUnavailable, err)
	}
	return nil
}

func mapKuboError(err error) error {
	if err == nil {
		return nil
	}
	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "not found") || strings.Contains(errStr, "does not exist") || strings.Contains(errStr, "no such file") {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	if strings.Contains(errStr, "connection refused") || strings.Contains(errStr, "daemon") {
		return fmt.Errorf("%w: %v", ErrDaemonUnavailable, err)
	}
	return err
}
