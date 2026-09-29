// Package ipfs implements a typed client for content-addressed storage
// built on the Kubo RPC API.
//
// Deterministic Adds:
// All content added via Store.Add and Store.AddDir uses CIDv1, raw leaves,
// and the default UnixFS chunker ("size-262144"). This guarantees that identical
// inputs yield identical CIDs across different nodes. Added content is pinned
// by default.
//
// Bounded Reads and Materialization:
// Store.Cat enforces byte limits while streaming, aborting with ErrTooLarge as
// soon as the limit is exceeded. Store.Fetch verifies Stat metadata and enforces
// limits (MaxBytes, MaxFiles, MaxDepth) during traversal, rejecting symlinks and
// directory escapes (ErrUnsafeContent) and cleaning up on failure.
//
// Garbage Collection:
// Store.Unpin unpins content from local storage but does not trigger Kubo garbage
// collection.
package ipfs
