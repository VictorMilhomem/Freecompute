package actors

import (
	"context"
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

type pollChainMsg struct{}

type RepsDirectMsg struct {
	Author string
	Reply  chan RepsResp
}

type cachedAction struct {
	author   string
	envelope *vcv1.Envelope
	skip     bool
}

type ChainBridge struct {
	act.Actor
	chain     ChainAPI
	cfg       BridgeConfig
	cache     map[string]cachedAction
	prevOrder []string
}

func NewChainBridge(chain ChainAPI, cfg BridgeConfig) *ChainBridge {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 500 * time.Millisecond
	}
	return &ChainBridge{
		chain: chain,
		cfg:   cfg,
		cache: make(map[string]cachedAction),
	}
}

func (b *ChainBridge) Init(args ...any) error {
	_, _ = b.SendAfter(b.PID(), pollChainMsg{}, 10*time.Millisecond)
	return nil
}

func (b *ChainBridge) HandleMessage(from gen.PID, message any) error {
	switch msg := message.(type) {
	case pollChainMsg:
		b.pollChain()
		_, _ = b.SendAfter(b.PID(), pollChainMsg{}, b.cfg.PollInterval)

	case RepsDirectMsg:
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		reps, err := b.chain.RepsAuthor(ctx, msg.Author)
		cancel()
		if msg.Reply != nil {
			msg.Reply <- RepsResp{Reps: reps, Err: err}
		}
	}
	return nil
}

func (b *ChainBridge) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	switch req := request.(type) {
	case PublishReq:
		data, err := proto.MarshalOptions{Deterministic: true}.Marshal(req.Envelope)
		if err != nil {
			return PublishResp{Err: err}, nil
		}
		id, err := b.chain.Post(ctx, data)
		if err != nil {
			return PublishResp{Err: err}, nil
		}
		for _, peer := range b.cfg.Peers {
			_ = b.chain.SyncSend(ctx, peer)
		}
		b.pollChain()
		return PublishResp{ActionID: id}, nil

	case VoteReq:
		var err error
		if req.Like {
			err = b.chain.LikeAuthor(ctx, req.Amount, req.Author)
		} else {
			err = b.chain.DislikeAuthor(ctx, req.Amount, req.Author)
		}
		if err == nil {
			for _, peer := range b.cfg.Peers {
				_ = b.chain.SyncSend(ctx, peer)
			}
		}
		return VoteResp{Err: err}, nil

	case RepsReq:
		reps, err := b.chain.RepsAuthor(ctx, req.Author)
		return RepsResp{Reps: reps, Err: err}, nil
	}
	return nil, nil
}

func (b *ChainBridge) pollChain() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	for _, peer := range b.cfg.Peers {
		_ = b.chain.SyncRecv(ctx, peer)
		_ = b.chain.SyncSend(ctx, peer)
	}

	order, err := b.chain.ListOrder(ctx)
	if err != nil {
		return
	}

	type resolvedItem struct {
		id    string
		entry protocol.Entry
		valid bool
	}

	resolved := make([]resolvedItem, 0, len(order))
	for idx, item := range order {
		if item.Revoked {
			continue
		}
		ca, ok := b.cache[item.ID]
		if !ok {
			meta, err := b.chain.GetMetadata(ctx, item.ID)
			if err != nil {
				return
			}
			if meta.Kind != "post" || meta.Signer == "" {
				b.cache[item.ID] = cachedAction{skip: true}
				continue
			}
			payload, err := b.chain.GetPayload(ctx, item.ID)
			if err != nil {
				return
			}
			env := &vcv1.Envelope{}
			if err := proto.Unmarshal(payload, env); err != nil {
				b.cache[item.ID] = cachedAction{skip: true}
				continue
			}
			ca = cachedAction{
				author:   meta.Signer,
				envelope: env,
			}
			b.cache[item.ID] = ca
		}
		if ca.skip {
			continue
		}
		resolved = append(resolved, resolvedItem{
			id: item.ID,
			entry: protocol.Entry{
				Index:    idx,
				Author:   ca.author,
				Envelope: ca.envelope,
			},
			valid: true,
		})
	}

	curIDs := make([]string, len(resolved))
	for i, r := range resolved {
		curIDs[i] = r.id
	}

	divIdx := 0
	for divIdx < len(b.prevOrder) && divIdx < len(curIDs) && b.prevOrder[divIdx] == curIDs[divIdx] {
		divIdx++
	}

	if divIdx == len(b.prevOrder) && divIdx == len(curIDs) {
		return
	}

	reset := divIdx < len(b.prevOrder)
	start := divIdx
	if reset {
		start = 0
	}

	entries := make([]protocol.Entry, 0, len(resolved)-start)
	for i := start; i < len(resolved); i++ {
		entries = append(entries, resolved[i].entry)
	}
	b.prevOrder = curIDs

	update := ChainUpdate{
		Reset:   reset,
		Entries: entries,
	}
	for _, target := range b.cfg.Targets {
		_ = b.Send(target, update)
	}
}
