package ipfs

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestLimitedReader(t *testing.T) {
	t.Run("reading within limit succeeds", func(t *testing.T) {
		data := "hello world"
		rc := io.NopCloser(strings.NewReader(data))
		lim := newLimitedReader(rc, 20)

		out, err := io.ReadAll(lim)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(out) != data {
			t.Errorf("got %q, want %q", string(out), data)
		}
	})

	t.Run("reading exact limit succeeds", func(t *testing.T) {
		data := "12345"
		rc := io.NopCloser(strings.NewReader(data))
		lim := newLimitedReader(rc, 5)

		out, err := io.ReadAll(lim)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(out) != data {
			t.Errorf("got %q, want %q", string(out), data)
		}
	})

	t.Run("fails with ErrTooLarge as soon as limit is exceeded", func(t *testing.T) {
		data := "1234567890EXTRA"
		rc := io.NopCloser(strings.NewReader(data))
		lim := newLimitedReader(rc, 10)

		buf := make([]byte, 100)
		_, err := lim.Read(buf)
		if err == nil {
			t.Fatal("expected ErrTooLarge, got nil")
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("expected ErrTooLarge, got %v", err)
		}
	})

	t.Run("chunked reads fail on exceeding byte", func(t *testing.T) {
		data := bytes.Repeat([]byte("a"), 15)
		rc := io.NopCloser(bytes.NewReader(data))
		lim := newLimitedReader(rc, 10)

		// Read 5 bytes
		buf := make([]byte, 5)
		n, err := lim.Read(buf)
		if err != nil || n != 5 {
			t.Fatalf("unexpected read: n=%d, err=%v", n, err)
		}

		// Read next 5 bytes
		n, err = lim.Read(buf)
		if err != nil || n != 5 {
			t.Fatalf("unexpected read: n=%d, err=%v", n, err)
		}

		// Try to read byte 11
		_, err = lim.Read(buf)
		if err == nil {
			t.Fatal("expected ErrTooLarge on byte 11, got nil")
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("expected ErrTooLarge, got %v", err)
		}
	})
}
