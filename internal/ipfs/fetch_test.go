package ipfs

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type mockNode struct {
	nodeType ContentNodeType
	name     string
	data     []byte
	children []ContentNode
}

func (m *mockNode) Type() ContentNodeType { return m.nodeType }
func (m *mockNode) Name() string          { return m.name }
func (m *mockNode) Size() int64           { return int64(len(m.data)) }
func (m *mockNode) Open() (io.ReadCloser, error) {
	if m.nodeType != NodeFile {
		return nil, ErrInvalidArgument
	}
	return io.NopCloser(bytes.NewReader(m.data)), nil
}
func (m *mockNode) Entries(ctx context.Context) ([]ContentNode, error) {
	if m.nodeType != NodeDir {
		return nil, ErrInvalidArgument
	}
	return m.children, nil
}

func TestFetch(t *testing.T) {
	ctx := context.Background()

	t.Run("successful directory materialization", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "target")
		root := &mockNode{
			nodeType: NodeDir,
			name:     "",
			children: []ContentNode{
				&mockNode{nodeType: NodeFile, name: "file1.txt", data: []byte("hello")},
				&mockNode{
					nodeType: NodeDir,
					name:     "subdir",
					children: []ContentNode{
						&mockNode{nodeType: NodeFile, name: "file2.txt", data: []byte("world")},
					},
				},
			},
		}

		res, err := safeMaterialize(ctx, root, dest, FetchLimits{MaxBytes: 1000, MaxFiles: 10, MaxDepth: 5})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Bytes != 10 || res.Files != 2 {
			t.Errorf("unexpected result: %+v", res)
		}

		b1, _ := os.ReadFile(filepath.Join(dest, "file1.txt"))
		if string(b1) != "hello" {
			t.Errorf("file1 content mismatch: %q", string(b1))
		}
		b2, _ := os.ReadFile(filepath.Join(dest, "subdir", "file2.txt"))
		if string(b2) != "world" {
			t.Errorf("file2 content mismatch: %q", string(b2))
		}
	})

	t.Run("reject non-empty destination", func(t *testing.T) {
		dest := t.TempDir()
		_ = os.WriteFile(filepath.Join(dest, "existing.txt"), []byte("data"), 0644)

		root := &mockNode{nodeType: NodeFile, name: "test.txt", data: []byte("abc")}
		_, err := safeMaterialize(ctx, root, dest, FetchLimits{})
		if err == nil {
			t.Fatal("expected error for non-empty dest, got nil")
		}
		if !errors.Is(err, ErrInvalidArgument) {
			t.Errorf("expected ErrInvalidArgument, got %v", err)
		}
	})

	t.Run("reject symlink and cleanup", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "cleanup-test")
		root := &mockNode{
			nodeType: NodeDir,
			children: []ContentNode{
				&mockNode{nodeType: NodeFile, name: "ok.txt", data: []byte("ok")},
				&mockNode{nodeType: NodeSymlink, name: "evil-link"},
			},
		}

		_, err := safeMaterialize(ctx, root, dest, FetchLimits{})
		if err == nil {
			t.Fatal("expected error on symlink, got nil")
		}
		if !errors.Is(err, ErrUnsafeContent) {
			t.Errorf("expected ErrUnsafeContent, got %v", err)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Errorf("expected dest %s to be cleaned up after error", dest)
		}
	})

	t.Run("reject path traversal escaping dest", func(t *testing.T) {
		traversals := []string{
			"../escape.txt",
			"..",
			"/etc/passwd",
			"sub/../../escape.txt",
		}

		for _, badName := range traversals {
			dest := filepath.Join(t.TempDir(), "traversal-test")
			root := &mockNode{
				nodeType: NodeDir,
				children: []ContentNode{
					&mockNode{nodeType: NodeFile, name: badName, data: []byte("bad")},
				},
			}
			_, err := safeMaterialize(ctx, root, dest, FetchLimits{})
			if err == nil {
				t.Fatalf("expected error for path traversal %q, got nil", badName)
			}
			if !errors.Is(err, ErrUnsafeContent) {
				t.Errorf("expected ErrUnsafeContent for %q, got %v", badName, err)
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Errorf("dest %s was not cleaned up after error for %q", dest, badName)
			}
		}
	})

	t.Run("enforce max depth", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "depth-test")
		// Depth 2: root -> sub1 -> sub2 -> file
		root := &mockNode{
			nodeType: NodeDir,
			children: []ContentNode{
				&mockNode{
					nodeType: NodeDir,
					name:     "sub1",
					children: []ContentNode{
						&mockNode{
							nodeType: NodeDir,
							name:     "sub2",
							children: []ContentNode{
								&mockNode{nodeType: NodeFile, name: "file.txt", data: []byte("deep")},
							},
						},
					},
				},
			},
		}

		_, err := safeMaterialize(ctx, root, dest, FetchLimits{MaxDepth: 1})
		if err == nil {
			t.Fatal("expected ErrTooLarge on exceeding max depth, got nil")
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("expected ErrTooLarge, got %v", err)
		}
	})

	t.Run("enforce max files", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "files-test")
		root := &mockNode{
			nodeType: NodeDir,
			children: []ContentNode{
				&mockNode{nodeType: NodeFile, name: "f1.txt", data: []byte("1")},
				&mockNode{nodeType: NodeFile, name: "f2.txt", data: []byte("2")},
			},
		}

		_, err := safeMaterialize(ctx, root, dest, FetchLimits{MaxFiles: 1})
		if err == nil {
			t.Fatal("expected ErrTooLarge on exceeding max files, got nil")
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("expected ErrTooLarge, got %v", err)
		}
	})

	t.Run("enforce max bytes while streaming", func(t *testing.T) {
		dest := filepath.Join(t.TempDir(), "bytes-test")
		root := &mockNode{
			nodeType: NodeFile,
			name:     "large.txt",
			data:     []byte("1234567890EXTRA"),
		}

		_, err := safeMaterialize(ctx, root, dest, FetchLimits{MaxBytes: 10})
		if err == nil {
			t.Fatal("expected ErrTooLarge on exceeding max bytes, got nil")
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("expected ErrTooLarge, got %v", err)
		}
		// Assert file was cleaned up
		if _, err := os.Stat(filepath.Join(dest, "large.txt")); !os.IsNotExist(err) {
			t.Error("partially written file was not removed")
		}
	})
}
