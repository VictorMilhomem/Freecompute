package chainbridge

import (
	"context"

	"github.com/VictorMilhomem/freecompute/freechains"
)

// ClientAdapter adapts *freechains.Client to the consumer-side ChainAPI interface.
type ClientAdapter struct {
	client *freechains.Client
}

// NewClientAdapter creates an adapter for *freechains.Client.
func NewClientAdapter(client *freechains.Client) *ClientAdapter {
	return &ClientAdapter{client: client}
}

func (a *ClientAdapter) Post(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error) {
	return a.client.Post(ctx, alias, payload, opts)
}

func (a *ClientAdapter) Like(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error {
	_, err := a.client.Like(ctx, alias, n, target, opts)
	return err
}

func (a *ClientAdapter) Dislike(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error {
	_, err := a.client.Dislike(ctx, alias, n, target, opts)
	return err
}

func (a *ClientAdapter) Revoke(ctx context.Context, alias freechains.Alias, n int64, id freechains.ActionID, opts freechains.VoteOptions) error {
	_, err := a.client.Revoke(ctx, alias, n, id, opts)
	return err
}

func (a *ClientAdapter) ListOrder(ctx context.Context, alias freechains.Alias) ([]freechains.OrderEntry, error) {
	return a.client.ListOrder(ctx, alias)
}

func (a *ClientAdapter) ListBegs(ctx context.Context, alias freechains.Alias) ([]freechains.ActionID, error) {
	return a.client.ListBegs(ctx, alias)
}

func (a *ClientAdapter) GetMetadata(ctx context.Context, alias freechains.Alias, id freechains.ActionID) (freechains.Metadata, error) {
	return a.client.GetMetadata(ctx, alias, id)
}

func (a *ClientAdapter) GetPayload(ctx context.Context, alias freechains.Alias, id freechains.ActionID) ([]byte, error) {
	return a.client.GetPayload(ctx, alias, id)
}

func (a *ClientAdapter) SyncRecv(ctx context.Context, alias freechains.Alias, remote string) error {
	return a.client.SyncRecv(ctx, alias, remote)
}

func (a *ClientAdapter) SyncSend(ctx context.Context, alias freechains.Alias, remote string) error {
	return a.client.SyncSend(ctx, alias, remote)
}
