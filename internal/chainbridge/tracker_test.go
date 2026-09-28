package chainbridge

import (
	"reflect"
	"testing"

	"github.com/VictorMilhomem/freecompute/freechains"
)

func makeEntry(id string, revoked bool) freechains.OrderEntry {
	return freechains.OrderEntry{
		ID:      freechains.ActionID(id),
		Revoked: revoked,
	}
}

func TestTrackerReconcile(t *testing.T) {
	a := makeEntry("1111111111111111111111111111111111111111", false)
	b := makeEntry("2222222222222222222222222222222222222222", false)
	c := makeEntry("3333333333333333333333333333333333333333", false)
	d := makeEntry("4444444444444444444444444444444444444444", false)
	x := makeEntry("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false)
	y := makeEntry("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", false)
	z := makeEntry("cccccccccccccccccccccccccccccccccccccccc", false)

	t.Run("first load", func(t *testing.T) {
		tracker := NewTracker()
		plan := tracker.Reconcile([]freechains.OrderEntry{a, b, c})

		if plan.Reorg != nil {
			t.Errorf("expected no reorg, got %+v", plan.Reorg)
		}
		if len(plan.RevokedActions) != 0 {
			t.Errorf("expected no revoked actions, got %v", plan.RevokedActions)
		}
		if len(plan.ToEmit) != 3 {
			t.Fatalf("expected 3 entries to emit, got %d", len(plan.ToEmit))
		}
		for i, expected := range []freechains.OrderEntry{a, b, c} {
			if plan.ToEmit[i].Index != i || plan.ToEmit[i].Entry != expected {
				t.Errorf("entry %d mismatch: got %+v, want index %d entry %+v", i, plan.ToEmit[i], i, expected)
			}
		}

		if !reflect.DeepEqual(tracker.Prefix(), []freechains.OrderEntry{a, b, c}) {
			t.Errorf("prefix mismatch: %+v", tracker.Prefix())
		}
	})

	t.Run("incremental append", func(t *testing.T) {
		tracker := NewTracker()
		tracker.Reconcile([]freechains.OrderEntry{a, b})

		plan := tracker.Reconcile([]freechains.OrderEntry{a, b, c})
		if plan.Reorg != nil {
			t.Errorf("expected no reorg, got %+v", plan.Reorg)
		}
		if len(plan.ToEmit) != 1 {
			t.Fatalf("expected 1 entry to emit, got %d", len(plan.ToEmit))
		}
		if plan.ToEmit[0].Index != 2 || plan.ToEmit[0].Entry != c {
			t.Errorf("unexpected toEmit: %+v", plan.ToEmit[0])
		}
		if !reflect.DeepEqual(tracker.Prefix(), []freechains.OrderEntry{a, b, c}) {
			t.Errorf("prefix mismatch: %+v", tracker.Prefix())
		}
	})

	t.Run("no-op when unchanged", func(t *testing.T) {
		tracker := NewTracker()
		tracker.Reconcile([]freechains.OrderEntry{a, b, c})

		plan := tracker.Reconcile([]freechains.OrderEntry{a, b, c})
		if plan.Reorg != nil {
			t.Errorf("expected no reorg, got %+v", plan.Reorg)
		}
		if len(plan.RevokedActions) != 0 {
			t.Errorf("expected no revoked actions, got %v", plan.RevokedActions)
		}
		if len(plan.ToEmit) != 0 {
			t.Errorf("expected nothing to emit, got %+v", plan.ToEmit)
		}
	})

	t.Run("reorg in the middle", func(t *testing.T) {
		tracker := NewTracker()
		tracker.Reconcile([]freechains.OrderEntry{a, b, c})

		// Fork at index 1: b is replaced by x, followed by y
		plan := tracker.Reconcile([]freechains.OrderEntry{a, x, y})
		if plan.Reorg == nil || plan.Reorg.FromIndex != 1 {
			t.Fatalf("expected reorg from index 1, got %+v", plan.Reorg)
		}
		if len(plan.ToEmit) != 2 {
			t.Fatalf("expected 2 entries to emit, got %d", len(plan.ToEmit))
		}
		if plan.ToEmit[0].Index != 1 || plan.ToEmit[0].Entry != x {
			t.Errorf("expected index 1 x, got %+v", plan.ToEmit[0])
		}
		if plan.ToEmit[1].Index != 2 || plan.ToEmit[1].Entry != y {
			t.Errorf("expected index 2 y, got %+v", plan.ToEmit[1])
		}
		if !reflect.DeepEqual(tracker.Prefix(), []freechains.OrderEntry{a, x, y}) {
			t.Errorf("prefix mismatch after reorg: %+v", tracker.Prefix())
		}
	})

	t.Run("reorg at index 0", func(t *testing.T) {
		tracker := NewTracker()
		tracker.Reconcile([]freechains.OrderEntry{a, b})

		// Total reorganization from genesis
		plan := tracker.Reconcile([]freechains.OrderEntry{z, b})
		if plan.Reorg == nil || plan.Reorg.FromIndex != 0 {
			t.Fatalf("expected reorg from index 0, got %+v", plan.Reorg)
		}
		if len(plan.ToEmit) != 2 {
			t.Fatalf("expected 2 entries to emit, got %d", len(plan.ToEmit))
		}
		if plan.ToEmit[0].Index != 0 || plan.ToEmit[0].Entry != z {
			t.Errorf("expected index 0 z, got %+v", plan.ToEmit[0])
		}
		if !reflect.DeepEqual(tracker.Prefix(), []freechains.OrderEntry{z, b}) {
			t.Errorf("prefix mismatch after reorg at 0: %+v", tracker.Prefix())
		}
	})

	t.Run("order shrinking", func(t *testing.T) {
		tracker := NewTracker()
		tracker.Reconcile([]freechains.OrderEntry{a, b, c, d})

		// Order shrank from 4 entries to 2 entries
		plan := tracker.Reconcile([]freechains.OrderEntry{a, b})
		if plan.Reorg == nil || plan.Reorg.FromIndex != 2 {
			t.Fatalf("expected reorg from index 2, got %+v", plan.Reorg)
		}
		if len(plan.ToEmit) != 0 {
			t.Errorf("expected 0 entries to emit on shrink, got %d", len(plan.ToEmit))
		}
		if !reflect.DeepEqual(tracker.Prefix(), []freechains.OrderEntry{a, b}) {
			t.Errorf("prefix mismatch after shrink: %+v", tracker.Prefix())
		}
	})

	t.Run("entry turning revoked", func(t *testing.T) {
		tracker := NewTracker()
		tracker.Reconcile([]freechains.OrderEntry{a, b, c})

		// Entry b turns revoked
		bRevoked := makeEntry(string(b.ID), true)
		plan := tracker.Reconcile([]freechains.OrderEntry{a, bRevoked, c})

		if plan.Reorg != nil {
			t.Errorf("expected no reorg for revoked transition, got %+v", plan.Reorg)
		}
		if len(plan.RevokedActions) != 1 || plan.RevokedActions[0] != b.ID {
			t.Fatalf("expected revoked actions [b.ID], got %v", plan.RevokedActions)
		}
		if len(plan.ToEmit) != 0 {
			t.Errorf("expected nothing to emit, got %+v", plan.ToEmit)
		}
		if !tracker.Prefix()[1].Revoked {
			t.Error("expected prefix[1].Revoked to be true")
		}

		// Subsequent poll with same state does not re-emit revoked
		plan2 := tracker.Reconcile([]freechains.OrderEntry{a, bRevoked, c})
		if len(plan2.RevokedActions) != 0 {
			t.Errorf("expected no duplicate revoked emission, got %v", plan2.RevokedActions)
		}
	})

	t.Run("duplicate ids in different positions", func(t *testing.T) {
		tracker := NewTracker()
		// Action a appears at index 0 and index 2
		plan := tracker.Reconcile([]freechains.OrderEntry{a, b, a})
		if len(plan.ToEmit) != 3 {
			t.Fatalf("expected 3 entries to emit, got %d", len(plan.ToEmit))
		}
		if plan.ToEmit[0].Index != 0 || plan.ToEmit[0].Entry.ID != a.ID {
			t.Errorf("unexpected entry 0: %+v", plan.ToEmit[0])
		}
		if plan.ToEmit[2].Index != 2 || plan.ToEmit[2].Entry.ID != a.ID {
			t.Errorf("unexpected entry 2: %+v", plan.ToEmit[2])
		}

		// Subsequent poll is a no-op
		plan2 := tracker.Reconcile([]freechains.OrderEntry{a, b, a})
		if len(plan2.ToEmit) != 0 {
			t.Errorf("expected no-op, got %d toEmit", len(plan2.ToEmit))
		}
	})
}
