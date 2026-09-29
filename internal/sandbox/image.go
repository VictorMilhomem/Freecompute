package sandbox

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// ImageRef represents a verified, digest-pinned container image reference.
type ImageRef string

// ParseImageRef validates and returns a digest-pinned ImageRef. Tags are strictly rejected.
func ParseImageRef(s string) (ImageRef, error) {
	if s == "" {
		return "", fmt.Errorf("%w: empty image reference", ErrImageNotPinned)
	}

	parts := strings.Split(s, "@sha256:")
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: missing @sha256: digest in %q", ErrImageNotPinned, s)
	}

	repo := parts[0]
	digest := parts[1]

	if repo == "" {
		return "", fmt.Errorf("%w: empty repository name in %q", ErrImageNotPinned, s)
	}

	// Reject tags in the repository portion (e.g. repo:tag@sha256:...)
	if strings.Contains(repo, ":") {
		return "", fmt.Errorf("%w: image reference %q contains tag; only pure digest references allowed", ErrImageNotPinned, s)
	}

	if len(digest) != 64 {
		return "", fmt.Errorf("%w: sha256 digest length %d is not 64 in %q", ErrImageNotPinned, len(digest), s)
	}

	if _, err := hex.DecodeString(digest); err != nil {
		return "", fmt.Errorf("%w: invalid hex in digest %q: %v", ErrImageNotPinned, digest, err)
	}

	return ImageRef(s), nil
}

// String returns the image reference string.
func (r ImageRef) String() string {
	return string(r)
}

// Digest returns the 64-character sha256 hex string.
func (r ImageRef) Digest() string {
	idx := strings.Index(string(r), "@sha256:")
	if idx < 0 {
		return ""
	}
	return string(r)[idx+8:]
}

// Repository returns the repository portion before the digest.
func (r ImageRef) Repository() string {
	idx := strings.Index(string(r), "@sha256:")
	if idx < 0 {
		return string(r)
	}
	return string(r)[:idx]
}
