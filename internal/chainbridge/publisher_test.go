package chainbridge

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/VictorMilhomem/freecompute/freechains"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

type mockPostAPI struct {
	ChainAPI
	postCalled bool
	lastPath   string
	fileExists bool
	postFunc   func(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error)
}

func (m *mockPostAPI) Post(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error) {
	m.postCalled = true
	m.lastPath = payload.Path()
	if _, err := os.Stat(payload.Path()); err == nil {
		m.fileExists = true
	}
	if m.postFunc != nil {
		return m.postFunc(ctx, alias, payload, opts)
	}
	return "1111111111111111111111111111111111111111", nil
}

func TestPublisher(t *testing.T) {
	alias := freechains.Alias("#chat")
	key := freechains.PrivateKeyPath("/path/to/key")
	ctx := context.Background()

	t.Run("oversized payload rejected before API call", func(t *testing.T) {
		api := &mockPostAPI{}
		// Set small limit of 20 bytes
		publisher := NewPublisher(api, alias, key, 20)

		env := &vcv1.Envelope{
			Version: 1,
			JobId:   strings.Repeat("a", 100),
		}

		_, err := publisher.Publish(ctx, env, false)
		if err == nil {
			t.Fatal("expected error for oversized payload, got nil")
		}
		if !errors.Is(err, freechains.ErrPayloadTooLarge) {
			t.Errorf("expected ErrPayloadTooLarge, got %v", err)
		}
		if api.postCalled {
			t.Error("Post should not have been called for oversized payload")
		}
	})

	t.Run("temp file removed on success and failure", func(t *testing.T) {
		env := &vcv1.Envelope{
			Version: 1,
			JobId:   "job-test",
		}

		// Success case
		apiSuccess := &mockPostAPI{}
		pubSuccess := NewPublisher(apiSuccess, alias, key, 1024)
		_, err := pubSuccess.Publish(ctx, env, false)
		if err != nil {
			t.Fatalf("unexpected error on publish: %v", err)
		}
		if !apiSuccess.fileExists {
			t.Fatal("temp file did not exist during Post call")
		}
		if _, err := os.Stat(apiSuccess.lastPath); !os.IsNotExist(err) {
			t.Errorf("temp file %s was not cleaned up after success", apiSuccess.lastPath)
		}

		// Failure case
		apiFailure := &mockPostAPI{
			postFunc: func(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error) {
				return "", errors.New("post failed")
			},
		}
		pubFailure := NewPublisher(apiFailure, alias, key, 1024)
		_, err = pubFailure.Publish(ctx, env, false)
		if err == nil {
			t.Fatal("expected error on failed post, got nil")
		}
		if !apiFailure.fileExists {
			t.Fatal("temp file did not exist during Post call")
		}
		if _, err := os.Stat(apiFailure.lastPath); !os.IsNotExist(err) {
			t.Errorf("temp file %s was not cleaned up after failure", apiFailure.lastPath)
		}
	})

	t.Run("beg option forwarded", func(t *testing.T) {
		var capturedOpts freechains.PostOptions
		api := &mockPostAPI{
			postFunc: func(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error) {
				capturedOpts = opts
				return "1111111111111111111111111111111111111111", nil
			},
		}

		publisher := NewPublisher(api, alias, key, 1024)
		env := &vcv1.Envelope{Version: 1, JobId: "beg-job"}

		_, err := publisher.Publish(ctx, env, true)
		if err != nil {
			t.Fatalf("unexpected publish error: %v", err)
		}
		if !capturedOpts.Beg {
			t.Errorf("expected Beg=true in PostOptions: %+v", capturedOpts)
		}
		if capturedOpts.Sign != "" {
			t.Errorf("expected empty Sign in PostOptions when Beg=true, got %s", capturedOpts.Sign)
		}
	})

	t.Run("insufficient reps propagated unchanged", func(t *testing.T) {
		api := &mockPostAPI{
			postFunc: func(ctx context.Context, alias freechains.Alias, payload freechains.Payload, opts freechains.PostOptions) (freechains.ActionID, error) {
				return "", freechains.ErrInsufficientReps
			},
		}

		publisher := NewPublisher(api, alias, key, 1024)
		env := &vcv1.Envelope{Version: 1, JobId: "reps-job"}

		_, err := publisher.Publish(ctx, env, false)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, freechains.ErrInsufficientReps) {
			t.Errorf("expected error wrapping ErrInsufficientReps, got %v", err)
		}
	})
}
