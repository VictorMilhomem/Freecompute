# sandbox

`internal/sandbox` provides an isolated runner for executing untrusted containers via the **Podman API** (libpod over `podman system service`).

## Isolation Guarantees

Every container is launched under strict isolation invariants enforced via `specgen.SpecGenerator`:
- **Rootless execution**: `Runner.Ping` verifies that the engine runs rootless when `Config.RequireRootless = true`.
- **User namespaces**: Drops root; runs as unprivileged UID/GID `65534:65534`.
- **Capability dropping**: Drops all Linux capabilities (`CapDrop = ["ALL"]`).
- **Privilege escalation prevention**: Enforces `no-new-privileges`.
- **Network isolation**: Completely disconnects networking (`NetNS = NoNetwork`).
- **Filesystem isolation**: Mounts root filesystem as read-only (`ReadOnlyFilesystem = true`).
- **Tmpfs**: Mounts a size-bounded, nodev, noexec, nosuid tmpfs on `/tmp`.
- **Mount constraints**: Bind mounts `InputDir` read-only at `/input` and `OutputDir` read-write at `/output`. No host devices, namespaces, or extra environment variables are permitted.
- **Resource ceilings**: Applies cgroup bounds on CPU quota, memory limits, and max PIDs. Specs requesting limits beyond `Config.HardLimits` are rejected immediately with `ErrLimitsExceeded`.
- **Output safety verification**: Walks `OutputDir` after execution to enforce size and file count bounds and strictly reject symlinks, devices, and non-regular files (`ErrUnsafeOutput`) before publishing results.
- **Guaranteed cleanup**: Removes containers forcefully in a defer with `Config.CleanupTimeout`. Startup cleanup clears orphaned containers bearing `vc.managed=true`.

## Build Tags

Compiling the Podman client bindings without requiring C development headers (e.g. `libbtrfs-dev`, `libgpgme-dev`, `libdevmapper-dev`) requires the following build tags:

```
exclude_graphdriver_btrfs btrfs_noversion exclude_graphdriver_devicemapper containers_image_openpgp
```
