package canon

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirectoryDigest(t *testing.T) {
	lim := Limits{
		MaxBytes: 10 * 1024 * 1024,
		MaxFiles: 1000,
	}

	t.Run("empty directory gives empty sha256", func(t *testing.T) {
		dir := t.TempDir()
		digest, err := DirectoryDigest(dir, lim)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expectedEmpty := sha256.Sum256(nil)
		if digest != expectedEmpty {
			t.Errorf("expected empty sha256 %x, got %x", expectedEmpty, digest)
		}
	})

	t.Run("known-answer test", func(t *testing.T) {
		dir := t.TempDir()

		// Write file "a.txt" with content "hello\n" (6 bytes)
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0644); err != nil {
			t.Fatal(err)
		}
		// Write nested file "sub/b.bin" with content "world" (5 bytes)
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sub", "b.bin"), []byte("world"), 0644); err != nil {
			t.Fatal(err)
		}

		// Expected hashed stream:
		// a.txt\n6\nhello\n\nsub/b.bin\n5\nworld\n
		h := sha256.New()
		h.Write([]byte("a.txt\n6\nhello\n\nsub/b.bin\n5\nworld\n"))
		var expectedKAT [32]byte
		copy(expectedKAT[:], h.Sum(nil))

		digest, err := DirectoryDigest(dir, lim)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if digest != expectedKAT {
			t.Errorf("KAT digest mismatch: want %s, got %s", hex.EncodeToString(expectedKAT[:]), hex.EncodeToString(digest[:]))
		}
	})

	t.Run("order and mtime invariance", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		// Dir 1: create file b first, then file a with past mtime
		_ = os.WriteFile(filepath.Join(dir1, "b.txt"), []byte("content b"), 0644)
		_ = os.WriteFile(filepath.Join(dir1, "a.txt"), []byte("content a"), 0600)
		_ = os.Chtimes(filepath.Join(dir1, "a.txt"), time.Unix(1000, 0), time.Unix(1000, 0))

		// Dir 2: create file a first, then file b with different mtime and permissions
		_ = os.WriteFile(filepath.Join(dir2, "a.txt"), []byte("content a"), 0666)
		_ = os.WriteFile(filepath.Join(dir2, "b.txt"), []byte("content b"), 0644)
		_ = os.Chtimes(filepath.Join(dir2, "b.txt"), time.Unix(2000, 0), time.Unix(2000, 0))

		d1, err := DirectoryDigest(dir1, lim)
		if err != nil {
			t.Fatalf("unexpected error on dir1: %v", err)
		}
		d2, err := DirectoryDigest(dir2, lim)
		if err != nil {
			t.Fatalf("unexpected error on dir2: %v", err)
		}
		if d1 != d2 {
			t.Errorf("expected identical digests regardless of creation order and mtime, got %x vs %x", d1, d2)
		}
	})

	t.Run("content change alters digest", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		_ = os.WriteFile(filepath.Join(dir1, "a.txt"), []byte("content 1"), 0644)
		_ = os.WriteFile(filepath.Join(dir2, "a.txt"), []byte("content 2"), 0644)

		d1, _ := DirectoryDigest(dir1, lim)
		d2, _ := DirectoryDigest(dir2, lim)
		if d1 == d2 {
			t.Error("expected different digests for different file content")
		}
	})

	t.Run("name change alters digest", func(t *testing.T) {
		dir1 := t.TempDir()
		dir2 := t.TempDir()

		_ = os.WriteFile(filepath.Join(dir1, "a.txt"), []byte("same content"), 0644)
		_ = os.WriteFile(filepath.Join(dir2, "b.txt"), []byte("same content"), 0644)

		d1, _ := DirectoryDigest(dir1, lim)
		d2, _ := DirectoryDigest(dir2, lim)
		if d1 == d2 {
			t.Error("expected different digests for different file names")
		}
	})

	t.Run("symlink rejected", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.txt")
		_ = os.WriteFile(target, []byte("target"), 0644)
		link := filepath.Join(dir, "link.txt")
		if err := os.Symlink(target, link); err != nil {
			t.Skip("symlinks not supported on this platform")
		}

		_, err := DirectoryDigest(dir, lim)
		if err == nil {
			t.Fatal("expected error on symlink, got nil")
		}
		if !errors.Is(err, ErrUnsafeEntry) {
			t.Errorf("expected ErrUnsafeEntry, got %v", err)
		}
	})

	t.Run("max files limit enforced", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "1.txt"), []byte("1"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "2.txt"), []byte("2"), 0644)

		_, err := DirectoryDigest(dir, Limits{MaxFiles: 1})
		if err == nil {
			t.Fatal("expected ErrTooLarge on exceeding file count, got nil")
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("expected ErrTooLarge, got %v", err)
		}
	})

	t.Run("max bytes limit enforced", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "1.txt"), []byte("1234567890"), 0644)

		_, err := DirectoryDigest(dir, Limits{MaxBytes: 5})
		if err == nil {
			t.Fatal("expected ErrTooLarge on exceeding max bytes, got nil")
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("expected ErrTooLarge, got %v", err)
		}
	})
}
