package chainbridge

import (
	"github.com/VictorMilhomem/freecompute/freechains"
)

// ActionToEmit represents an action entry scheduled to be emitted at an index.
type ActionToEmit struct {
	Index int
	Entry freechains.OrderEntry
}

// Plan specifies the actions needed to synchronize with an updated consensus order.
type Plan struct {
	Reorg          *Reorg
	RevokedActions []freechains.ActionID
	ToEmit         []ActionToEmit
}

// Tracker maintains the emitted prefix of consensus order to order and deduplicate actions.
type Tracker struct {
	prefix []freechains.OrderEntry
}

// NewTracker constructs an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{}
}

// Prefix returns a snapshot of the current emitted prefix.
func (t *Tracker) Prefix() []freechains.OrderEntry {
	out := make([]freechains.OrderEntry, len(t.prefix))
	copy(out, t.prefix)
	return out
}

// Reconcile computes the plan to synchronize state with the newly observed order.
func (t *Tracker) Reconcile(order []freechains.OrderEntry) Plan {
	var plan Plan

	divergentIndex := -1
	minLen := len(t.prefix)
	if len(order) < minLen {
		minLen = len(order)
	}

	for i := 0; i < minLen; i++ {
		if t.prefix[i].ID != order[i].ID {
			divergentIndex = i
			break
		}
	}

	if divergentIndex == -1 && len(order) < len(t.prefix) {
		divergentIndex = len(order)
	}

	if divergentIndex != -1 {
		plan.Reorg = &Reorg{FromIndex: divergentIndex}
		t.prefix = t.prefix[:divergentIndex]

		for i := divergentIndex; i < len(order); i++ {
			plan.ToEmit = append(plan.ToEmit, ActionToEmit{
				Index: i,
				Entry: order[i],
			})
			t.prefix = append(t.prefix, order[i])
		}
		return plan
	}

	for i := 0; i < len(t.prefix); i++ {
		if !t.prefix[i].Revoked && order[i].Revoked {
			t.prefix[i].Revoked = true
			plan.RevokedActions = append(plan.RevokedActions, order[i].ID)
		}
	}

	for i := len(t.prefix); i < len(order); i++ {
		plan.ToEmit = append(plan.ToEmit, ActionToEmit{
			Index: i,
			Entry: order[i],
		})
		t.prefix = append(t.prefix, order[i])
	}

	return plan
}
