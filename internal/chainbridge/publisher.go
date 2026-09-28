package chainbridge

import (
	"context"
	"fmt"
	"os"

	"github.com/VictorMilhomem/freecompute/freechains"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
	"google.golang.org/protobuf/proto"
)

// Publisher handles envelope marshaling, size validation, temp files, and publishing.
type Publisher struct {
	api          ChainAPI
	alias        freechains.Alias
	signingKey   freechains.PrivateKeyPath
	maxPostBytes int
}

// NewPublisher creates a new Publisher component.
func NewPublisher(api ChainAPI, alias freechains.Alias, signingKey freechains.PrivateKeyPath, maxPostBytes int) *Publisher {
	if maxPostBytes <= 0 {
		maxPostBytes = freechains.MaxPayloadSize
	}
	return &Publisher{
		api:          api,
		alias:        alias,
		signingKey:   signingKey,
		maxPostBytes: maxPostBytes,
	}
}

// Publish marshals an envelope, enforces size bounds, writes to a temp file, calls Post, and ensures cleanup.
func (p *Publisher) Publish(ctx context.Context, env *vcv1.Envelope, beg bool) (freechains.ActionID, error) {
	if env == nil {
		return "", fmt.Errorf("%w: envelope cannot be nil", freechains.ErrInvalidArgument)
	}

	data, err := proto.Marshal(env)
	if err != nil {
		return "", fmt.Errorf("%w: failed to marshal envelope: %v", ErrInvalidEnvelope, err)
	}

	if len(data) > p.maxPostBytes {
		return "", fmt.Errorf("%w: envelope size %d exceeds %d bytes", freechains.ErrPayloadTooLarge, len(data), p.maxPostBytes)
	}

	tmpFile, err := os.CreateTemp("", "chainbridge-post-*.bin")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary post file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return "", fmt.Errorf("failed to write post data to temporary file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("failed to close temporary post file: %w", err)
	}

	opts := freechains.PostOptions{
		Beg: beg,
	}
	if !beg {
		opts.Sign = p.signingKey
	}

	return p.api.Post(ctx, p.alias, freechains.FilePayload(tmpPath), opts)
}
