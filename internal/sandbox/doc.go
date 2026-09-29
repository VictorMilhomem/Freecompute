// Package sandbox implements a typed runner for untrusted containers
// using the Podman API (libpod, served by podman system service).
//
// Isolation Invariants:
//   - Rootless engine execution (enforced by default via Ping).
//   - Complete network isolation (no network access).
//   - Read-only root filesystem with a size-bounded tmpfs mounted at /tmp.
//   - All Linux capabilities dropped ("ALL") and no-new-privileges set.
//   - Non-root container user (UID/GID 65534:65534).
//   - Mandatory cgroup resource bounds on CPU, memory, and max PIDs.
//   - Strictly isolated volume mounts: InputDir mounted read-only at /input,
//     and OutputDir mounted read-write at /output. No host devices, namespaces,
//     or unapproved environment variables.
//   - Managed container labeling: vc.managed=true applied to every container.
//
// Limit Governance:
// Job announcements are untrusted. Spec limits that exceed Config.HardLimits
// are rejected immediately with ErrLimitsExceeded (never silently clamped).
// Zero-valued limits are populated from Config.DefaultLimits.
//
// Output Verification:
// After execution, the output directory is walked to enforce size and file
// count bounds and reject symlinks, sockets, devices, and non-regular files
// (ErrUnsafeOutput) before content is hashed and committed to IPFS.
//
// Cleanup:
// Containers are always forcefully removed in a defer using a fresh context
// with Config.CleanupTimeout. Runner.Cleanup removes all orphaned containers
// carrying vc.managed=true.
package sandbox
