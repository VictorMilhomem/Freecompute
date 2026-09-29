package sandbox

import (
	"errors"
	"testing"
)

func TestParseImageRef(t *testing.T) {
	validDigest := "docker.io/library/alpine@sha256:e7d88de73db3d3a9b58339f1f4096306354fa13ac2048fe55e3d09d6b279e430"

	t.Run("valid digest reference accepted", func(t *testing.T) {
		ref, err := ParseImageRef(validDigest)
		if err != nil {
			t.Fatalf("unexpected error parsing valid image ref: %v", err)
		}
		if ref.String() != validDigest {
			t.Errorf("got %s, want %s", ref.String(), validDigest)
		}
		if ref.Repository() != "docker.io/library/alpine" {
			t.Errorf("unexpected repository: %s", ref.Repository())
		}
		if ref.Digest() != "e7d88de73db3d3a9b58339f1f4096306354fa13ac2048fe55e3d09d6b279e430" {
			t.Errorf("unexpected digest: %s", ref.Digest())
		}
	})

	t.Run("tag strictly rejected", func(t *testing.T) {
		tags := []string{
			"alpine:latest",
			"docker.io/library/alpine:3.18",
			"docker.io/library/alpine:latest@sha256:e7d88de73db3d3a9b58339f1f4096306354fa13ac2048fe55e3d09d6b279e430",
		}
		for _, s := range tags {
			_, err := ParseImageRef(s)
			if err == nil {
				t.Errorf("expected error for tagged image ref %q, got nil", s)
			}
			if !errors.Is(err, ErrImageNotPinned) {
				t.Errorf("expected ErrImageNotPinned for %q, got %v", s, err)
			}
		}
	})

	t.Run("malformed digest rejected", func(t *testing.T) {
		malformed := []string{
			"",
			"alpine",
			"alpine@",
			"alpine@sha256:",
			"alpine@sha256:1234", // too short
			"alpine@sha256:zzzd88de73db3d3a9b58339f1f4096306354fa13ac2048fe55e3d09d6b279e430", // invalid hex
			"@sha256:e7d88de73db3d3a9b58339f1f4096306354fa13ac2048fe55e3d09d6b279e430",        // empty repo
		}
		for _, s := range malformed {
			_, err := ParseImageRef(s)
			if err == nil {
				t.Errorf("expected error for malformed image ref %q, got nil", s)
			}
			if !errors.Is(err, ErrImageNotPinned) {
				t.Errorf("expected ErrImageNotPinned for %q, got %v", s, err)
			}
		}
	})
}
