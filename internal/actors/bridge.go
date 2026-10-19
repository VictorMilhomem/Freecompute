package actors

import (
	"context"
	"fmt"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/internal/protocol"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
	"google.golang.org/protobuf/proto"
)

type BridgeConfig struct {
	Peers        []string
	PollInterval time.Duration
	Targets      []gen.Atom
}

type pollTick struct{}

type cachedAction struct {
	author   string
	envelope *vcv1.Envelope
}

type ChainBridge struct {
	act.Actor

	api     ChainAPI
	cfg     BridgeConfig
	cache   map[string]cachedAction
	skipped map[string]bool
}

func NewChainBridge(api ChainAPI, cfg BridgeConfig) *ChainBridge {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	return &ChainBridge{
		api:     api,
		cfg:     cfg,
		cache:   make(map[string]cachedAction),
		skipped: make(map[string]bool),
	}
}

func (b *ChainBridge) Init(args ...any) error {
	_ = b.Send(b.PID(), pollTick{})
	return nil
}

func (b *ChainBridge) HandleMessage(from gen.PID, message any) error {
	switch message.(type) {
	case pollTick:
		b.syncAndPoll(context.Background())
		_, _ = b.SendAfter(b.PID(), pollTick{}, b.cfg.PollInterval)
	}
	return nil
}

func (b *ChainBridge) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	ctx := context.Background()
	switch req := request.(type) {
	case PublishReq:
		if req.Envelope == nil {
			return PublishResp{Err: fmt.Errorf("envelope cannot be nil")}, nil
		}
		data, err := proto.MarshalOptions{Deterministic: true}.Marshal(req.Envelope)
		if err != nil {
			return PublishResp{Err: err}, nil
		}
		id, err := b.api.Post(ctx, data)
		if err == nil {
			b.syncAndPoll(ctx)
		}
		return PublishResp{ActionID: id, Err: err}, nil

	case VoteReq:
		var err error
		if req.Like {
			err = b.api.LikeAuthor(ctx, req.Amount, req.Author)
		} else {
			err = b.api.DislikeAuthor(ctx, req.Amount, req.Author)
		}
		if err == nil {
			b.syncAndPoll(ctx)
		}
		return VoteResp{Err: err}, nil

	default:
		return nil, fmt.Errorf("unknown request type %T", request)
	}
}

func (b *ChainBridge) syncAndPoll(ctx context.Context) {
	for _, peer := range b.cfg.Peers {
		if peer == "" {
			continue
		}
		if err := b.api.SyncRecv(ctx, peer); err != nil {
			b.Log().Warning("sync recv from %s failed: %v", peer, err)
			continue
		}
		if err := b.api.SyncSend(ctx, peer); err != nil {
			b.Log().Warning("sync send to %s failed: %v", peer, err)
		}
	}

	order, err := b.api.ListOrder(ctx)
	if err != nil {
		b.Log().Warning("list order failed: %v", err)
		return
	}

	var entries []protocol.Entry
	for idx, item := range order {
		if item.Revoked || b.skipped[item.ID] {
			continue
		}

		cached, ok := b.cache[item.ID]
		if !ok {
			meta, err := b.api.GetMetadata(ctx, item.ID)
			if err != nil {
				b.Log().Warning("get metadata %s failed: %v", item.ID, err)
				continue
			}
			if meta.Kind != "post" || meta.Signer == "" {
				b.skipped[item.ID] = true
				continue
			}
			payload, err := b.api.GetPayload(ctx, item.ID)
			if err != nil {
				b.Log().Warning("get payload %s failed: %v", item.ID, err)
				continue
			}
			var env vcv1.Envelope
			if err := proto.Unmarshal(payload, &env); err != nil || env.Version != 1 {
				b.skipped[item.ID] = true
				continue
			}
			cached = cachedAction{
				author:   meta.Signer,
				envelope: &env,
			}
			b.cache[item.ID] = cached
		}

		entries = append(entries, protocol.Entry{
			Index:    idx,
			Author:   cached.author,
			Envelope: cached.envelope,
		})
	}

	update := ChainUpdate{Entries: entries}
	for _, target := range b.cfg.Targets {
		_ = b.Send(target, update)
	}
}
