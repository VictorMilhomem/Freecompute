package chainbridge

import (
	"context"
	"errors"
	"testing"

	"github.com/VictorMilhomem/freecompute/freechains"
)

type mockSyncAPI struct {
	ChainAPI
	recvCalls map[string]int
	sendCalls map[string]int

	recvFails map[string]error
	sendFails map[string]error
}

func (m *mockSyncAPI) SyncRecv(ctx context.Context, alias freechains.Alias, remote string) error {
	m.recvCalls[remote]++
	if err, ok := m.recvFails[remote]; ok {
		return err
	}
	return nil
}

func (m *mockSyncAPI) SyncSend(ctx context.Context, alias freechains.Alias, remote string) error {
	m.sendCalls[remote]++
	if err, ok := m.sendFails[remote]; ok {
		return err
	}
	return nil
}

func TestSyncer(t *testing.T) {
	api := &mockSyncAPI{
		recvCalls: make(map[string]int),
		sendCalls: make(map[string]int),
		recvFails: map[string]error{
			"peer-failing-recv": errors.New("network unreachable"),
		},
		sendFails: map[string]error{
			"peer-failing-send": errors.New("connection reset"),
		},
	}

	peers := []string{
		"peer-success-1",
		"peer-failing-recv",
		"peer-failing-send",
		"peer-success-2",
	}

	syncer := NewSyncer(api, "#compute", peers)
	ctx := context.Background()

	failures := syncer.SyncAll(ctx)

	// All 4 peers must have been attempted
	if api.recvCalls["peer-success-1"] != 1 || api.sendCalls["peer-success-1"] != 1 {
		t.Errorf("peer-success-1 was not fully synced")
	}
	if api.recvCalls["peer-failing-recv"] != 1 {
		t.Errorf("peer-failing-recv was not attempted")
	}
	if api.sendCalls["peer-failing-recv"] != 0 {
		t.Errorf("SyncSend should not have been called if SyncRecv failed")
	}
	if api.recvCalls["peer-failing-send"] != 1 || api.sendCalls["peer-failing-send"] != 1 {
		t.Errorf("peer-failing-send was not attempted")
	}
	if api.recvCalls["peer-success-2"] != 1 || api.sendCalls["peer-success-2"] != 1 {
		t.Errorf("peer-success-2 was not fully synced despite prior failures")
	}

	// Failures must be reported for the 2 failing peers
	if len(failures) != 2 {
		t.Fatalf("expected 2 failures, got %d", len(failures))
	}
	if failures[0].Peer != "peer-failing-recv" {
		t.Errorf("expected peer-failing-recv failure, got %s", failures[0].Peer)
	}
	if failures[1].Peer != "peer-failing-send" {
		t.Errorf("expected peer-failing-send failure, got %s", failures[1].Peer)
	}
}
