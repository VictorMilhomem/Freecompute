//go:build integration
// +build integration

package ipfs

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIntegrationKubo(t *testing.T) {
	addr := os.Getenv("IPFS_API_ADDR")
	if addr == "" {
		addr = "http://127.0.0.1:5001"
	}

	cfg := Config{
		APIAddr:        addr,
		RequestTimeout: 10 * time.Second,
	}

	store, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	ctx := context.Background()
	if err := store.Ping(ctx); err != nil {
		t.Skipf("skipping integration test: Kubo daemon not available at %s: %v", addr, err)
	}

	t.Run("add, stat, cat, pin, and fetch", func(t *testing.T) {
		content := []byte("integration test content for ipfs store")
		cid, err := store.Add(ctx, bytes.NewReader(content))
		if err != nil {
			t.Fatalf("Add failed: %v", err)
		}

		st, err := store.Stat(ctx, cid)
		if err != nil {
			t.Fatalf("Stat failed: %v", err)
		}
		if st.Size != uint64(len(content)) {
			t.Errorf("expected size %d, got %d", len(content), st.Size)
		}

		rc, err := store.Cat(ctx, cid, 1024)
		if err != nil {
			t.Fatalf("Cat failed: %v", err)
		}
		defer rc.Close()

		readBack, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("reading Cat stream failed: %v", err)
		}
		if string(readBack) != string(content) {
			t.Errorf("Cat content mismatch: %q", string(readBack))
		}

		isPinned, err := store.IsPinned(ctx, cid)
		if err != nil || !isPinned {
			t.Errorf("expected content to be pinned by default, got %v (err=%v)", isPinned, err)
		}

		dest := filepath.Join(t.TempDir(), "materialized")
		res, err := store.Fetch(ctx, cid, dest, FetchLimits{MaxBytes: 1024, MaxFiles: 10, MaxDepth: 5})
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if res.Bytes != int64(len(content)) {
			t.Errorf("expected %d bytes, got %d", len(content), res.Bytes)
		}
	})
}
