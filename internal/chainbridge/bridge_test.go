package chainbridge

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/VictorMilhomem/freecompute/freechains"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
	"google.golang.org/protobuf/proto"
)

type fakeEventSink struct {
	events []Event
}

func (s *fakeEventSink) Deliver(event Event) {
	s.events = append(s.events, event)
}

type fakeBridgeAPI struct {
	postFunc     func(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error)
	likeFunc     func(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error
	dislikeFunc  func(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error
	revokeFunc   func(ctx context.Context, alias freechains.Alias, n int64, id freechains.ActionID, opts freechains.VoteOptions) error
	listOrder    []freechains.OrderEntry
	listBegs     []freechains.ActionID
	metadataMap  map[freechains.ActionID]freechains.Metadata
	payloadMap   map[freechains.ActionID][]byte
	payloadFails map[freechains.ActionID]error
	syncRecvFail map[string]error
	syncSendFail map[string]error

	payloadFetched map[freechains.ActionID]bool
}

func newFakeBridgeAPI() *fakeBridgeAPI {
	return &fakeBridgeAPI{
		metadataMap:    make(map[freechains.ActionID]freechains.Metadata),
		payloadMap:     make(map[freechains.ActionID][]byte),
		payloadFails:   make(map[freechains.ActionID]error),
		syncRecvFail:   make(map[string]error),
		syncSendFail:   make(map[string]error),
		payloadFetched: make(map[freechains.ActionID]bool),
	}
}

func (f *fakeBridgeAPI) Post(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error) {
	if f.postFunc != nil {
		return f.postFunc(ctx, alias, payload, opts)
	}
	return "1111111111111111111111111111111111111111", nil
}

func (f *fakeBridgeAPI) Like(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error {
	if f.likeFunc != nil {
		return f.likeFunc(ctx, alias, n, target, opts)
	}
	return nil
}

func (f *fakeBridgeAPI) Dislike(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error {
	if f.dislikeFunc != nil {
		return f.dislikeFunc(ctx, alias, n, target, opts)
	}
	return nil
}

func (f *fakeBridgeAPI) Revoke(ctx context.Context, alias freechains.Alias, n int64, id freechains.ActionID, opts freechains.VoteOptions) error {
	if f.revokeFunc != nil {
		return f.revokeFunc(ctx, alias, n, id, opts)
	}
	return nil
}

func (f *fakeBridgeAPI) ListOrder(ctx context.Context, alias freechains.Alias) ([]freechains.OrderEntry, error) {
	return f.listOrder, nil
}

func (f *fakeBridgeAPI) ListBegs(ctx context.Context, alias freechains.Alias) ([]freechains.ActionID, error) {
	return f.listBegs, nil
}

func (f *fakeBridgeAPI) GetMetadata(ctx context.Context, alias freechains.Alias, id freechains.ActionID) (freechains.Metadata, error) {
	if meta, ok := f.metadataMap[id]; ok {
		return meta, nil
	}
	return freechains.Metadata{ID: id, Kind: freechains.ActionPost, Time: 123456789}, nil
}

func (f *fakeBridgeAPI) GetPayload(ctx context.Context, alias freechains.Alias, id freechains.ActionID) ([]byte, error) {
	f.payloadFetched[id] = true
	if err, ok := f.payloadFails[id]; ok {
		return nil, err
	}
	if data, ok := f.payloadMap[id]; ok {
		return data, nil
	}
	return nil, freechains.ErrActionNotFound
}

func (f *fakeBridgeAPI) SyncRecv(ctx context.Context, alias freechains.Alias, remote string) error {
	if err, ok := f.syncRecvFail[remote]; ok {
		return err
	}
	return nil
}

func (f *fakeBridgeAPI) SyncSend(ctx context.Context, alias freechains.Alias, remote string) error {
	if err, ok := f.syncSendFail[remote]; ok {
		return err
	}
	return nil
}

func makeValidEnvelopeBytes(t *testing.T, jobID string) []byte {
	t.Helper()
	env := &vcv1.Envelope{
		Version: 1,
		JobId:   jobID,
	}
	data, err := proto.Marshal(env)
	if err != nil {
		t.Fatalf("failed to marshal envelope: %v", err)
	}
	return data
}

func TestChainBridge(t *testing.T) {
	ctx := context.Background()
	defaultCfg := Config{
		Alias:            "#compute",
		SigningKey:       "/path/to/key",
		PollInterval:     time.Second,
		SyncInterval:     time.Second,
		SupportedVersion: 1,
	}

	t.Run("publish flow", func(t *testing.T) {
		api := newFakeBridgeAPI()
		sink := &fakeEventSink{}
		bridge, err := NewChainBridge(api, sink, defaultCfg)
		if err != nil {
			t.Fatalf("failed to create bridge: %v", err)
		}

		expectedID := freechains.ActionID("1111111111111111111111111111111111111111")
		var capturedBeg bool
		api.postFunc = func(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error) {
			capturedBeg = opts.Beg
			return expectedID, nil
		}

		env := &vcv1.Envelope{Version: 1, JobId: "job-1"}

		// Signed publish
		resp := bridge.HandlePublish(ctx, PublishRequest{Envelope: env, Beg: false})
		if resp.Err != nil || resp.ActionID != expectedID {
			t.Fatalf("unexpected publish response: %+v", resp)
		}
		if capturedBeg {
			t.Errorf("expected Beg=false")
		}

		// Beg publish
		resp = bridge.HandlePublish(ctx, PublishRequest{Envelope: env, Beg: true})
		if resp.Err != nil || resp.ActionID != expectedID {
			t.Fatalf("unexpected publish response: %+v", resp)
		}
		if !capturedBeg {
			t.Errorf("expected Beg=true")
		}
	})

	t.Run("poll emitting new actions in order", func(t *testing.T) {
		api := newFakeBridgeAPI()
		sink := &fakeEventSink{}
		bridge, _ := NewChainBridge(api, sink, defaultCfg)

		id1 := freechains.ActionID("1111111111111111111111111111111111111111")
		id2 := freechains.ActionID("2222222222222222222222222222222222222222")

		api.listOrder = []freechains.OrderEntry{
			{ID: id1, Revoked: false},
			{ID: id2, Revoked: false},
		}

		signer := freechains.PublicKey("ssh-ed25519 AAA-signer")
		api.metadataMap[id1] = freechains.Metadata{ID: id1, Kind: freechains.ActionPost, Time: 100, Signer: &signer}
		api.metadataMap[id2] = freechains.Metadata{ID: id2, Kind: freechains.ActionPost, Time: 200, Signer: &signer}

		api.payloadMap[id1] = makeValidEnvelopeBytes(t, "job-1")
		api.payloadMap[id2] = makeValidEnvelopeBytes(t, "job-2")

		bridge.HandlePoll(ctx)

		if len(sink.events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(sink.events))
		}

		ev1, ok := sink.events[0].(Appended)
		if !ok || ev1.Index != 0 || ev1.ActionID != id1 || ev1.LocalTime != 100 || ev1.Envelope.JobId != "job-1" {
			t.Errorf("unexpected event 0: %+v", sink.events[0])
		}

		ev2, ok := sink.events[1].(Appended)
		if !ok || ev2.Index != 1 || ev2.ActionID != id2 || ev2.LocalTime != 200 || ev2.Envelope.JobId != "job-2" {
			t.Errorf("unexpected event 1: %+v", sink.events[1])
		}
	})

	t.Run("malformed payload yielding Discarded while later actions still emitted", func(t *testing.T) {
		api := newFakeBridgeAPI()
		sink := &fakeEventSink{}
		bridge, _ := NewChainBridge(api, sink, defaultCfg)

		id1 := freechains.ActionID("1111111111111111111111111111111111111111")
		id2 := freechains.ActionID("2222222222222222222222222222222222222222")

		api.listOrder = []freechains.OrderEntry{
			{ID: id1, Revoked: false},
			{ID: id2, Revoked: false},
		}

		api.payloadMap[id1] = []byte("corrupt-bytes-not-proto")
		api.payloadMap[id2] = makeValidEnvelopeBytes(t, "job-2")

		bridge.HandlePoll(ctx)

		if len(sink.events) != 2 {
			t.Fatalf("expected 2 events, got %d: %+v", len(sink.events), sink.events)
		}

		disc, ok := sink.events[0].(Discarded)
		if !ok || disc.ActionID != id1 || !errors.Is(disc.Reason, ErrInvalidEnvelope) {
			t.Errorf("expected Discarded for id1, got %+v", sink.events[0])
		}

		app, ok := sink.events[1].(Appended)
		if !ok || app.ActionID != id2 || app.Index != 1 {
			t.Errorf("expected Appended for id2, got %+v", sink.events[1])
		}
	})

	t.Run("revoked entry yielding Revoked without fetching payload", func(t *testing.T) {
		api := newFakeBridgeAPI()
		sink := &fakeEventSink{}
		bridge, _ := NewChainBridge(api, sink, defaultCfg)

		revokedID := freechains.ActionID("3333333333333333333333333333333333333333")
		api.listOrder = []freechains.OrderEntry{
			{ID: revokedID, Revoked: true},
		}

		bridge.HandlePoll(ctx)

		if len(sink.events) != 1 {
			t.Fatalf("expected 1 event, got %d", len(sink.events))
		}

		rev, ok := sink.events[0].(Revoked)
		if !ok || rev.ActionID != revokedID {
			t.Errorf("expected Revoked event, got %+v", sink.events[0])
		}

		if api.payloadFetched[revokedID] {
			t.Errorf("GetPayload should not be called for an entry marked Revoked=true in list order")
		}
	})

	t.Run("reorg yielding Reorg then re-emission", func(t *testing.T) {
		api := newFakeBridgeAPI()
		sink := &fakeEventSink{}
		bridge, _ := NewChainBridge(api, sink, defaultCfg)

		id1 := freechains.ActionID("1111111111111111111111111111111111111111")
		id2 := freechains.ActionID("2222222222222222222222222222222222222222")
		idFork := freechains.ActionID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

		api.listOrder = []freechains.OrderEntry{
			{ID: id1, Revoked: false},
			{ID: id2, Revoked: false},
		}
		api.payloadMap[id1] = makeValidEnvelopeBytes(t, "job-1")
		api.payloadMap[id2] = makeValidEnvelopeBytes(t, "job-2")
		api.payloadMap[idFork] = makeValidEnvelopeBytes(t, "job-fork")

		// First poll emits id1, id2
		bridge.HandlePoll(ctx)
		sink.events = nil

		// Fork at index 1: id2 replaced by idFork
		api.listOrder = []freechains.OrderEntry{
			{ID: id1, Revoked: false},
			{ID: idFork, Revoked: false},
		}

		bridge.HandlePoll(ctx)

		if len(sink.events) != 2 {
			t.Fatalf("expected 2 events after reorg, got %d: %+v", len(sink.events), sink.events)
		}

		reorg, ok := sink.events[0].(Reorg)
		if !ok || reorg.FromIndex != 1 {
			t.Errorf("expected Reorg{FromIndex: 1}, got %+v", sink.events[0])
		}

		app, ok := sink.events[1].(Appended)
		if !ok || app.Index != 1 || app.ActionID != idFork {
			t.Errorf("expected Appended for idFork at index 1, got %+v", sink.events[1])
		}
	})

	t.Run("vote and revoke pass-through", func(t *testing.T) {
		api := newFakeBridgeAPI()
		sink := &fakeEventSink{}
		bridge, _ := NewChainBridge(api, sink, defaultCfg)

		var capturedKind string
		var capturedAmount int64
		api.likeFunc = func(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error {
			capturedKind = "like"
			capturedAmount = n
			return nil
		}
		api.dislikeFunc = func(ctx context.Context, alias freechains.Alias, n int64, target freechains.Target, opts freechains.VoteOptions) error {
			capturedKind = "dislike"
			capturedAmount = n
			return nil
		}
		api.revokeFunc = func(ctx context.Context, alias freechains.Alias, n int64, id freechains.ActionID, opts freechains.VoteOptions) error {
			capturedKind = "revoke"
			capturedAmount = n
			return nil
		}

		targetAct := freechains.ActionTarget("1111111111111111111111111111111111111111")

		// Like
		vResp := bridge.HandleVote(ctx, VoteRequest{Kind: VoteLike, Amount: 100, Target: targetAct, Why: "nice"})
		if vResp.Err != nil || capturedKind != "like" || capturedAmount != 100 {
			t.Errorf("like failed: %+v", vResp)
		}

		// Dislike
		vResp = bridge.HandleVote(ctx, VoteRequest{Kind: VoteDislike, Amount: 50, Target: targetAct, Why: "spam"})
		if vResp.Err != nil || capturedKind != "dislike" || capturedAmount != 50 {
			t.Errorf("dislike failed: %+v", vResp)
		}

		// Revoke
		rResp := bridge.HandleRevoke(ctx, RevokeRequest{Amount: 500, ActionID: "1111111111111111111111111111111111111111", Why: "abuse"})
		if rResp.Err != nil || capturedKind != "revoke" || capturedAmount != 500 {
			t.Errorf("revoke failed: %+v", rResp)
		}
	})

	t.Run("sync failure yielding SyncFailed", func(t *testing.T) {
		api := newFakeBridgeAPI()
		sink := &fakeEventSink{}
		cfg := defaultCfg
		cfg.Peers = []string{"peer-good", "peer-bad"}

		bridge, _ := NewChainBridge(api, sink, cfg)

		syncErr := errors.New("timeout connecting to peer")
		api.syncRecvFail["peer-bad"] = syncErr

		bridge.HandleSync(ctx)

		if len(sink.events) != 1 {
			t.Fatalf("expected 1 SyncFailed event, got %d", len(sink.events))
		}

		failed, ok := sink.events[0].(SyncFailed)
		if !ok || failed.Peer != "peer-bad" || !errors.Is(failed.Err, syncErr) {
			t.Errorf("unexpected SyncFailed event: %+v", sink.events[0])
		}
	})

	t.Run("adapter verification", func(t *testing.T) {
		// Verify ClientAdapter implements ChainAPI
		var _ ChainAPI = (*ClientAdapter)(nil)
		if reflect.TypeOf((*ClientAdapter)(nil)).Implements(reflect.TypeOf((*ChainAPI)(nil)).Elem()) != true {
			t.Fatal("ClientAdapter does not implement ChainAPI")
		}
	})
}
