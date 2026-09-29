package ipfs

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

type mockRPCAPI struct {
	addFunc      func(ctx context.Context, r io.Reader) (CID, error)
	addDirFunc   func(ctx context.Context, dir string) (CID, error)
	getFunc      func(ctx context.Context, id CID) (ContentNode, error)
	statFunc     func(ctx context.Context, id CID) (Stat, error)
	pinFunc      func(ctx context.Context, id CID) error
	unpinFunc    func(ctx context.Context, id CID) error
	isPinnedFunc func(ctx context.Context, id CID) (bool, error)
	pingFunc     func(ctx context.Context) error
}

func (m *mockRPCAPI) Add(ctx context.Context, r io.Reader) (CID, error) {
	if m.addFunc != nil {
		return m.addFunc(ctx, r)
	}
	return "bafybeicg2abbgahf5oz3whswsqx5qjfs4xedr4eamrf7eefk672ea63p3y", nil
}

func (m *mockRPCAPI) AddDir(ctx context.Context, dir string) (CID, error) {
	if m.addDirFunc != nil {
		return m.addDirFunc(ctx, dir)
	}
	return "bafybeicg2abbgahf5oz3whswsqx5qjfs4xedr4eamrf7eefk672ea63p3y", nil
}

func (m *mockRPCAPI) Get(ctx context.Context, id CID) (ContentNode, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return &mockNode{nodeType: NodeFile, name: "file.txt", data: []byte("content")}, nil
}

func (m *mockRPCAPI) Stat(ctx context.Context, id CID) (Stat, error) {
	if m.statFunc != nil {
		return m.statFunc(ctx, id)
	}
	return Stat{Size: 7, IsDir: false, NumLinks: 1}, nil
}

func (m *mockRPCAPI) Pin(ctx context.Context, id CID) error {
	if m.pinFunc != nil {
		return m.pinFunc(ctx, id)
	}
	return nil
}

func (m *mockRPCAPI) Unpin(ctx context.Context, id CID) error {
	if m.unpinFunc != nil {
		return m.unpinFunc(ctx, id)
	}
	return nil
}

func (m *mockRPCAPI) IsPinned(ctx context.Context, id CID) (bool, error) {
	if m.isPinnedFunc != nil {
		return m.isPinnedFunc(ctx, id)
	}
	return true, nil
}

func (m *mockRPCAPI) Ping(ctx context.Context) error {
	if m.pingFunc != nil {
		return m.pingFunc(ctx)
	}
	return nil
}

func newTestStore(api rpcAPI, cfg Config) *ipfsStore {
	return &ipfsStore{api: api, cfg: cfg}
}

func TestStore(t *testing.T) {
	cidValid := CID("bafybeicg2abbgahf5oz3whswsqx5qjfs4xedr4eamrf7eefk672ea63p3y")
	ctx := context.Background()

	t.Run("address validation blocks remote by default", func(t *testing.T) {
		cfgRemote := Config{
			APIAddr:     "http://192.168.1.50:5001",
			AllowRemote: false,
		}
		if err := cfgRemote.Validate(); err == nil {
			t.Fatal("expected error for remote address, got nil")
		}

		cfgLoopback := Config{
			APIAddr:     "http://127.0.0.1:5001",
			AllowRemote: false,
		}
		if err := cfgLoopback.Validate(); err != nil {
			t.Fatalf("unexpected error for loopback address: %v", err)
		}

		cfgAllowedRemote := Config{
			APIAddr:     "http://192.168.1.50:5001",
			AllowRemote: true,
		}
		if err := cfgAllowedRemote.Validate(); err != nil {
			t.Fatalf("unexpected error when AllowRemote=true: %v", err)
		}
	})

	t.Run("Cat stopping at byte limit", func(t *testing.T) {
		mock := &mockRPCAPI{
			getFunc: func(ctx context.Context, id CID) (ContentNode, error) {
				return &mockNode{
					nodeType: NodeFile,
					name:     "test",
					data:     []byte("1234567890EXTRA"),
				}, nil
			},
		}
		store := newTestStore(mock, Config{})
		rc, err := store.Cat(ctx, cidValid, 10)
		if err != nil {
			t.Fatalf("unexpected error opening cat: %v", err)
		}
		defer rc.Close()

		buf := make([]byte, 100)
		_, err = rc.Read(buf)
		if err == nil {
			t.Fatal("expected ErrTooLarge from cat stream, got nil")
		}
		if !errors.Is(err, ErrTooLarge) {
			t.Errorf("expected ErrTooLarge, got %v", err)
		}
	})

	t.Run("timeout mapping", func(t *testing.T) {
		mock := &mockRPCAPI{
			statFunc: func(ctx context.Context, id CID) (Stat, error) {
				return Stat{}, context.DeadlineExceeded
			},
		}
		store := newTestStore(mock, Config{RequestTimeout: time.Millisecond})
		_, err := store.Stat(ctx, cidValid)
		if err == nil {
			t.Fatal("expected error on timeout, got nil")
		}
		if !errors.Is(err, ErrTimeout) {
			t.Errorf("expected ErrTimeout, got %v", err)
		}
	})

	t.Run("pin and unpin flow", func(t *testing.T) {
		var pinned bool
		mock := &mockRPCAPI{
			pinFunc: func(ctx context.Context, id CID) error {
				pinned = true
				return nil
			},
			unpinFunc: func(ctx context.Context, id CID) error {
				pinned = false
				return nil
			},
			isPinnedFunc: func(ctx context.Context, id CID) (bool, error) {
				return pinned, nil
			},
		}
		store := newTestStore(mock, Config{})

		if err := store.Pin(ctx, cidValid); err != nil {
			t.Fatalf("pin failed: %v", err)
		}
		isP, err := store.IsPinned(ctx, cidValid)
		if err != nil || !isP {
			t.Fatalf("expected pinned=true, got %v (err=%v)", isP, err)
		}

		if err := store.Unpin(ctx, cidValid); err != nil {
			t.Fatalf("unpin failed: %v", err)
		}
		isP, err = store.IsPinned(ctx, cidValid)
		if err != nil || isP {
			t.Fatalf("expected pinned=false, got %v (err=%v)", isP, err)
		}
	})

	t.Run("ping daemon unavailable mapping", func(t *testing.T) {
		mock := &mockRPCAPI{
			pingFunc: func(ctx context.Context) error {
				return ErrDaemonUnavailable
			},
		}
		store := newTestStore(mock, Config{})
		if err := store.Ping(ctx); !errors.Is(err, ErrDaemonUnavailable) {
			t.Errorf("expected ErrDaemonUnavailable, got %v", err)
		}
	})
}
