package ipfs

import "errors"

var (
	// ErrInvalidCID indicates a malformed or undefined CID.
	ErrInvalidCID = errors.New("invalid CID")

	// ErrNotFound indicates the requested content does not exist.
	ErrNotFound = errors.New("content not found")

	// ErrTimeout indicates an operation exceeded its allotted deadline.
	ErrTimeout = errors.New("operation timed out")

	// ErrTooLarge indicates content exceeded the maximum allowed size or depth limit.
	ErrTooLarge = errors.New("content exceeds size limit")

	// ErrUnsafeContent indicates an unsafe file entry (symlink, device, escaping path).
	ErrUnsafeContent = errors.New("unsafe content detected")

	// ErrDaemonUnavailable indicates connection to the Kubo RPC API failed.
	ErrDaemonUnavailable = errors.New("IPFS daemon unavailable")

	// ErrInvalidArgument indicates caller passed invalid parameters.
	ErrInvalidArgument = errors.New("invalid argument")
)
