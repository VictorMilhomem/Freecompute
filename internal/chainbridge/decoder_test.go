package chainbridge

import (
	"errors"
	"testing"

	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
	"google.golang.org/protobuf/proto"
)

func TestEnvelopeDecoder(t *testing.T) {
	decoder := NewEnvelopeDecoder(1)

	t.Run("valid envelope", func(t *testing.T) {
		env := &vcv1.Envelope{
			Version: 1,
			JobId:   "job-12345",
			Body: &vcv1.Envelope_Announce{
				Announce: &vcv1.JobAnnounce{
					ImageRef: "docker.io/library/alpine@sha256:abcdef",
					Cmd:      []string{"echo", "hello"},
				},
			},
		}
		data, err := proto.Marshal(env)
		if err != nil {
			t.Fatalf("failed to marshal valid envelope: %v", err)
		}

		decoded, err := decoder.Decode(data)
		if err != nil {
			t.Fatalf("unexpected error decoding: %v", err)
		}
		if decoded.Version != 1 || decoded.JobId != "job-12345" {
			t.Errorf("decoded envelope mismatch: %+v", decoded)
		}
	})

	t.Run("garbage bytes", func(t *testing.T) {
		garbage := []byte{0xff, 0xfe, 0xfd, 0x12, 0x34}
		_, err := decoder.Decode(garbage)
		if err == nil {
			t.Fatal("expected error on garbage bytes, got nil")
		}
		if !errors.Is(err, ErrInvalidEnvelope) {
			t.Errorf("expected ErrInvalidEnvelope, got %v", err)
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		env := &vcv1.Envelope{
			Version: 2, // expected 1
			JobId:   "job-999",
		}
		data, err := proto.Marshal(env)
		if err != nil {
			t.Fatalf("failed to marshal envelope: %v", err)
		}

		_, err = decoder.Decode(data)
		if err == nil {
			t.Fatal("expected error on unsupported version, got nil")
		}
		if !errors.Is(err, ErrUnsupportedVersion) {
			t.Errorf("expected ErrUnsupportedVersion, got %v", err)
		}
	})

	t.Run("empty payload", func(t *testing.T) {
		_, err := decoder.Decode([]byte{})
		if err == nil {
			t.Fatal("expected error on empty bytes, got nil")
		}
		if !errors.Is(err, ErrInvalidEnvelope) {
			t.Errorf("expected ErrInvalidEnvelope, got %v", err)
		}
	})
}
