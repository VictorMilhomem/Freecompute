package chainbridge

import (
	"fmt"

	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
	"google.golang.org/protobuf/proto"
)

// EnvelopeDecoder decodes bytes into an Envelope, validating protocol version.
type EnvelopeDecoder struct {
	supportedVersion uint32
}

// NewEnvelopeDecoder creates an EnvelopeDecoder for the expected protocol version.
func NewEnvelopeDecoder(supportedVersion uint32) *EnvelopeDecoder {
	return &EnvelopeDecoder{supportedVersion: supportedVersion}
}

// Decode unmarshals protobuf data and checks protocol version compatibility.
func (d *EnvelopeDecoder) Decode(data []byte) (*vcv1.Envelope, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty payload data", ErrInvalidEnvelope)
	}

	var env vcv1.Envelope
	if err := proto.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidEnvelope, err)
	}

	if env.Version != d.supportedVersion {
		return nil, fmt.Errorf("%w: got %d, expected %d", ErrUnsupportedVersion, env.Version, d.supportedVersion)
	}

	return &env, nil
}
