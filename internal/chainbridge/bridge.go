package chainbridge

import (
	"context"
	"errors"
	"fmt"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/VictorMilhomem/freecompute/freechains"
)

// ChainBridge is the Ergo actor providing the single point of contact between
// the volunteer computing system and the Freechains chain.
type ChainBridge struct {
	act.Actor

	api       ChainAPI
	sink      EventSink
	cfg       Config
	tracker   *Tracker
	decoder   *EnvelopeDecoder
	publisher *Publisher
	syncer    *Syncer
}

// NewChainBridge constructs a new ChainBridge actor with validated configuration.
func NewChainBridge(api ChainAPI, sink EventSink, cfg Config) (*ChainBridge, error) {
	if api == nil {
		return nil, fmt.Errorf("%w: chain api cannot be nil", freechains.ErrInvalidArgument)
	}
	if sink == nil {
		return nil, fmt.Errorf("%w: event sink cannot be nil", freechains.ErrInvalidArgument)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	tracker := NewTracker()
	decoder := NewEnvelopeDecoder(cfg.SupportedVersion)
	publisher := NewPublisher(api, cfg.Alias, cfg.SigningKey, cfg.MaxPostBytes)
	syncer := NewSyncer(api, cfg.Alias, cfg.Peers)

	return &ChainBridge{
		api:       api,
		sink:      sink,
		cfg:       cfg,
		tracker:   tracker,
		decoder:   decoder,
		publisher: publisher,
		syncer:    syncer,
	}, nil
}

// Init is called by Ergo when the actor process starts.
func (b *ChainBridge) Init(args ...any) error {
	if b.Process != nil {
		b.Log().Info("chainbridge starting for alias %s", b.cfg.Alias)
	}

	ctx := context.Background()
	b.HandlePoll(ctx)

	b.schedulePoll()
	b.scheduleSync()
	return nil
}

func (b *ChainBridge) schedulePoll() {
	if b.Process != nil && b.PID() != (gen.PID{}) && b.cfg.PollInterval > 0 {
		_, _ = b.SendAfter(b.PID(), pollTick{}, b.cfg.PollInterval)
	}
}

func (b *ChainBridge) scheduleSync() {
	if b.Process != nil && b.PID() != (gen.PID{}) && b.cfg.SyncInterval > 0 {
		_, _ = b.SendAfter(b.PID(), syncTick{}, b.cfg.SyncInterval)
	}
}

// HandleMessage handles asynchronous Ergo messages (timers).
func (b *ChainBridge) HandleMessage(from gen.PID, message any) error {
	ctx := context.Background()
	switch message.(type) {
	case pollTick:
		b.HandlePoll(ctx)
		b.schedulePoll()
	case syncTick:
		b.HandleSync(ctx)
		b.scheduleSync()
	default:
		if b.Process != nil {
			b.Log().Warning("chainbridge received unknown message from %v", from)
		}
	}
	return nil
}

// HandleCall handles synchronous requests (publish, vote, revoke).
func (b *ChainBridge) HandleCall(from gen.PID, ref gen.Ref, request any) (any, error) {
	ctx := context.Background()
	switch req := request.(type) {
	case PublishRequest:
		return b.HandlePublish(ctx, req), nil
	case VoteRequest:
		return b.HandleVote(ctx, req), nil
	case RevokeRequest:
		return b.HandleRevoke(ctx, req), nil
	default:
		return nil, fmt.Errorf("%w: unknown call request: %T", freechains.ErrInvalidArgument, request)
	}
}

// HandlePublish processes an envelope publication request.
func (b *ChainBridge) HandlePublish(ctx context.Context, req PublishRequest) PublishResponse {
	id, err := b.publisher.Publish(ctx, req.Envelope, req.Beg)
	return PublishResponse{
		ActionID: id,
		Err:      err,
	}
}

// HandleVote processes a like or dislike vote request.
func (b *ChainBridge) HandleVote(ctx context.Context, req VoteRequest) VoteResponse {
	opts := freechains.VoteOptions{
		Sign: b.cfg.SigningKey,
		Why:  req.Why,
	}
	var err error
	switch req.Kind {
	case VoteLike:
		err = b.api.Like(ctx, b.cfg.Alias, req.Amount, req.Target, opts)
	case VoteDislike:
		err = b.api.Dislike(ctx, b.cfg.Alias, req.Amount, req.Target, opts)
	default:
		err = fmt.Errorf("%w: unknown vote kind %d", freechains.ErrInvalidArgument, req.Kind)
	}
	return VoteResponse{Err: err}
}

// HandleRevoke processes an action revocation request.
func (b *ChainBridge) HandleRevoke(ctx context.Context, req RevokeRequest) RevokeResponse {
	opts := freechains.VoteOptions{
		Sign: b.cfg.SigningKey,
		Why:  req.Why,
	}
	err := b.api.Revoke(ctx, b.cfg.Alias, req.Amount, req.ActionID, opts)
	return RevokeResponse{Err: err}
}

// HandlePoll queries the consensus order and emits appropriate events.
func (b *ChainBridge) HandlePoll(ctx context.Context) {
	order, err := b.api.ListOrder(ctx, b.cfg.Alias)
	if err != nil {
		if b.Process != nil {
			b.Log().Warning("chainbridge poll list order failed: %v", err)
		}
		return
	}

	plan := b.tracker.Reconcile(order)

	if plan.Reorg != nil {
		b.sink.Deliver(*plan.Reorg)
	}

	for _, actID := range plan.RevokedActions {
		b.sink.Deliver(Revoked{ActionID: actID})
	}

	for _, item := range plan.ToEmit {
		if item.Entry.Revoked {
			b.sink.Deliver(Revoked{ActionID: item.Entry.ID})
			continue
		}

		meta, err := b.api.GetMetadata(ctx, b.cfg.Alias, item.Entry.ID)
		if err != nil {
			b.sink.Deliver(Discarded{
				ActionID: item.Entry.ID,
				Reason:   fmt.Errorf("failed to fetch metadata: %w", err),
			})
			continue
		}

		payload, err := b.api.GetPayload(ctx, b.cfg.Alias, item.Entry.ID)
		if err != nil {
			if errors.Is(err, freechains.ErrPayloadRevoked) {
				b.sink.Deliver(Revoked{ActionID: item.Entry.ID})
			} else {
				b.sink.Deliver(Discarded{
					ActionID: item.Entry.ID,
					Reason:   fmt.Errorf("failed to fetch payload: %w", err),
				})
			}
			continue
		}

		env, err := b.decoder.Decode(payload)
		if err != nil {
			b.sink.Deliver(Discarded{
				ActionID: item.Entry.ID,
				Reason:   err,
			})
			continue
		}

		var author freechains.PublicKey
		if meta.Signer != nil {
			author = *meta.Signer
		}

		b.sink.Deliver(Appended{
			Index:     item.Index,
			ActionID:  item.Entry.ID,
			Author:    author,
			LocalTime: meta.Time,
			Envelope:  env,
		})
	}
}

// HandleSync coordinates periodic peer synchronization.
func (b *ChainBridge) HandleSync(ctx context.Context) {
	failures := b.syncer.SyncAll(ctx)
	for _, f := range failures {
		b.sink.Deliver(SyncFailed{
			Peer: f.Peer,
			Err:  f.Err,
		})
	}
}
