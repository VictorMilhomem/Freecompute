package freecompute

import "errors"

var (
	ErrNoQuorum         = errors.New("freecompute: no quorum reached")
	ErrJobExpired       = errors.New("freecompute: job expired")
	ErrInsufficientReps = errors.New("freecompute: insufficient reputation")
	ErrImageNotAllowed  = errors.New("freecompute: image not allowed")
	ErrInvalidSpec      = errors.New("freecompute: invalid specification")
	ErrNodeStopped      = errors.New("freecompute: node stopped")
)
