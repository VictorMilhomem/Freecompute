package chainbridge

import "errors"

var (
	// ErrUnsupportedVersion is returned when an envelope has an unexpected protocol version.
	ErrUnsupportedVersion = errors.New("unsupported envelope version")

	// ErrInvalidEnvelope is returned when decoding protobuf bytes fails.
	ErrInvalidEnvelope = errors.New("invalid envelope protobuf")
)
