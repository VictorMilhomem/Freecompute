package sandbox

import "errors"

var (
	// ErrImageNotPinned indicates an image reference was not pinned by digest.
	ErrImageNotPinned = errors.New("image reference not pinned by digest")

	// ErrImageNotAllowed indicates an image was rejected by the security policy.
	ErrImageNotAllowed = errors.New("image not allowed by policy")

	// ErrLimitsExceeded indicates requested resource limits exceed hard ceilings.
	ErrLimitsExceeded = errors.New("resource limits exceed hard ceiling")

	// ErrTimeout indicates container execution timed out.
	ErrTimeout = errors.New("container execution timed out")

	// ErrOOMKilled indicates the container was terminated by the OOM killer.
	ErrOOMKilled = errors.New("container killed by OOM")

	// ErrOutputTooLarge indicates the output directory exceeded byte or file limits.
	ErrOutputTooLarge = errors.New("output exceeds size or file count limit")

	// ErrUnsafeOutput indicates an unsafe file (symlink, socket, device) in output directory.
	ErrUnsafeOutput = errors.New("unsafe file in output directory")

	// ErrNotRootless indicates the Podman engine is running as root when rootless was required.
	ErrNotRootless = errors.New("podman engine is not rootless")

	// ErrEngineUnavailable indicates the Podman service socket is unreachable.
	ErrEngineUnavailable = errors.New("podman engine unavailable")

	// ErrInvalidArgument indicates invalid parameters were passed.
	ErrInvalidArgument = errors.New("invalid argument")
)
