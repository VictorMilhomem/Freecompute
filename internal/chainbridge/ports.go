package chainbridge

import (
	"context"

	"github.com/VictorMilhomem/freecompute/freechains"
)

// ChainAPI defines the subset of Freechains operations required by ChainBridge.
type ChainAPI interface {
	Post(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error)
	Like(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error
	Dislike(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error
	Revoke(ctx context.Context, alias freechains.Alias, n int64, id freechains.ActionID, opts freechains.VoteOptions) error
	ListOrder(ctx context.Context, alias freechains.Alias) ([]freechains.OrderEntry, error)
	ListBegs(ctx context.Context, alias freechains.Alias) ([]freechains.ActionID, error)
	GetMetadata(ctx context.Context, alias freechains.Alias, id freechains.ActionID) (freechains.Metadata, error)
	GetPayload(ctx context.Context, alias freechains.Alias, id freechains.ActionID) ([]byte, error)
	SyncRecv(ctx context.Context, alias freechains.Alias, remote string) error
	SyncSend(ctx context.Context, alias freechains.Alias, remote string) error
}

// EventSink consumes events delivered by ChainBridge.
type EventSink interface {
	Deliver(event Event)
}
