package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyOutputDir(t *testing.T) {
	t.Run("valid regular output files", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "out1.dat"), []byte("12345"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dir, "nested"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "nested", "out2.dat"), []byte("67890"), 0644); err != nil {
			t.Fatal(err)
		}

		bytes, files, err := VerifyOutputDir(dir, 100, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if bytes != 10 || files != 2 {
			t.Errorf("expected 10 bytes and 2 files, got %d bytes and %d files", bytes, files)
		}
	})

	t.Run("symlink rejected with ErrUnsafeOutput", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "target.txt")
		_ = os.WriteFile(target, []byte("target"), 0644)
		link := filepath.Join(dir, "evil_symlink")
		if err := os.Symlink(target, link); err != nil {
			t.Skip("symlinks not supported on this platform")
		}

		_, _, err := VerifyOutputDir(dir, 100, 10)
		if err == nil {
			t.Fatal("expected error for symlink, got nil")
		}
		if !errors.Is(err, ErrUnsafeOutput) {
			t.Errorf("expected ErrUnsafeOutput, got %v", err)
		}
	})

	t.Run("exceeding max output files", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "1.dat"), []byte("1"), 0644)
		_ = os.WriteFile(filepath.Join(dir, "2.dat"), []byte("2"), 0644)

		_, _, err := VerifyOutputDir(dir, 100, 1)
		if err == nil {
			t.Fatal("expected ErrOutputTooLarge on too many files, got nil")
		}
		if !errors.Is(err, ErrOutputTooLarge) {
			t.Errorf("expected ErrOutputTooLarge, got %v", err)
		}
	})

	t.Run("exceeding max output bytes", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "1.dat"), []byte("12345678901"), 0644)

		_, _, err := VerifyOutputDir(dir, 10, 10)
		if err == nil {
			t.Fatal("expected ErrOutputTooLarge on oversize bytes, got nil")
		}
		if !errors.Is(err, ErrOutputTooLarge) {
			t.Errorf("expected ErrOutputTooLarge, got %v", err)
		}
	})
}
