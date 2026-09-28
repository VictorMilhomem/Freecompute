package chainbridge

import (
	"github.com/VictorMilhomem/freecompute/freechains"
	vcv1 "github.com/VictorMilhomem/freecompute/vc/v1"
)

// PublishRequest requests the bridge to marshal and publish an envelope to the chain.
type PublishRequest struct {
	Envelope *vcv1.Envelope
	Beg      bool
}

// PublishResponse contains the result of a PublishRequest.
type PublishResponse struct {
	ActionID freechains.ActionID
	Err      error
}

// VoteKind specifies whether a vote is an upvote or downvote.
type VoteKind int

const (
	VoteLike VoteKind = iota
	VoteDislike
)

// VoteRequest requests the bridge to cast a like or dislike vote.
type VoteRequest struct {
	Kind   VoteKind
	Amount int64
	Target freechains.Target
	Why    string
}

// VoteResponse contains the result of a VoteRequest.
type VoteResponse struct {
	Err error
}

// RevokeRequest requests the bridge to revoke an action's payload.
type RevokeRequest struct {
	Amount   int64
	ActionID freechains.ActionID
	Why      string
}

// RevokeResponse contains the result of a RevokeRequest.
type RevokeResponse struct {
	Err error
}

type pollTick struct{}
type syncTick struct{}
