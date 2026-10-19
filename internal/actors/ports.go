package actors

import (
	"context"
	"io"

	"github.com/VictorMilhomem/freecompute/internal/protocol"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

type OrderItem struct {
	ID      string
	Revoked bool
}

type ActionMeta struct {
	ID     string
	Kind   string
	Signer string
	Time   int64
}

type ChainAPI interface {
	Post(ctx context.Context, payload []byte) (string, error)
	ListOrder(ctx context.Context) ([]OrderItem, error)
	GetMetadata(ctx context.Context, id string) (ActionMeta, error)
	GetPayload(ctx context.Context, id string) ([]byte, error)
	LikeAuthor(ctx context.Context, amount int64, authorPub string) error
	DislikeAuthor(ctx context.Context, amount int64, authorPub string) error
	SyncRecv(ctx context.Context, remote string) error
	SyncSend(ctx context.Context, remote string) error
}

type Store interface {
	Add(ctx context.Context, r io.Reader) (string, error)
	AddDir(ctx context.Context, dir string) (string, error)
	Fetch(ctx context.Context, cid string, dest string) error
	Ping(ctx context.Context) error
}

type Runner interface {
	Run(ctx context.Context, imageRef string, cmd []string, inputDir, outputDir string) (int32, error)
	Cleanup(ctx context.Context) error
	Ping(ctx context.Context) error
}

type ChainUpdate struct {
	Entries []protocol.Entry
}

type PublishReq struct {
	Envelope *vcv1.Envelope
}

type PublishResp struct {
	ActionID string
	Err      error
}

type VoteReq struct {
	Like   bool
	Amount int64
	Author string
}

type VoteResp struct {
	Err error
}
