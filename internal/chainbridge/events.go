package chainbridge

import (
	"github.com/VictorMilhomem/freecompute/freechains"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

// Event represents a sealed sum type for events emitted by ChainBridge.
type Event interface {
	isBridgeEvent()
}

// Appended is emitted when a valid action is confirmed at a consensus position.
type Appended struct {
	Index    int
	ActionID freechains.ActionID
	Author   freechains.PublicKey
	// LocalTime is the author's local clock and is not trustworthy. Exposed for inspection only.
	LocalTime int64
	Envelope  *vcv1.Envelope
}

func (Appended) isBridgeEvent() {}

// Reorg is emitted when consensus order diverges from the previously emitted prefix.
type Reorg struct {
	FromIndex int
}

func (Reorg) isBridgeEvent() {}

// Revoked is emitted when an action's payload is or becomes revoked.
type Revoked struct {
	ActionID freechains.ActionID
}

func (Revoked) isBridgeEvent() {}

// Discarded is emitted when an action payload cannot be fetched, parsed, or validated.
type Discarded struct {
	ActionID freechains.ActionID
	Reason   error
}

func (Discarded) isBridgeEvent() {}

// SyncFailed is emitted when synchronization with a remote peer fails.
type SyncFailed struct {
	Peer string
	Err  error
}

func (SyncFailed) isBridgeEvent() {}
