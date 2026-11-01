# Reference Manual

This document provides exhaustive configuration, flag, environment variable, and component references for Freecompute.

---

## CLI Reference

### Binaries

- `bin/vcnode [run] [flags]`: Production Freecompute daemon.
- `bin/vcnode-faulty [run] [flags]`: Fault-injection Worker (built only via `make build-faulty` with `//go:build faulty`). Injects `999\n` into `result.txt` to test quorum rejection and dislike penalties.

Both binaries share the startup logic in [`internal/cli/cli.go`](../internal/cli/cli.go).

### Command-Line Flags

| Flag | Type | Default | Description |
| :--- | :--- | :--- | :--- |
| `--id` | `string` | *(required)* | Node identifier (e.g. `alice`, `bob`). |
| `--root` | `string` | *(required)* | Freechains root directory path. |
| `--key` | `string` | *(required)* | Path to Ed25519 private key (`<key>.pub` must exist). |
| `--chain` | `string` | `#vc` | Freechains chain alias. |
| `--peers` | `string` | `""` | Comma-separated `host:port` peer addresses. |
| `--state-dir` | `string` | `""` (`<root>/vc-state`) | Worker crash-recovery directory. |
| `--allow-image` | `string` (repeatable / CSV) | `nil` | Digest-pinned image reference added to the Worker allowlist. |
| `--allow-any-image` | `bool` | `false` | Allow arbitrary digest-pinned images (development only). |
| `--allow-own-jobs` | `bool` | `false` | Allow Worker to process jobs submitted by this node's key. |
| `--submit` | `string` | `""` | Input file path; if set, submits a job, prints report, and exits. |

---

## Environment Variables

| Variable | Default | Used By | Description |
| :--- | :--- | :--- | :--- |
| `VC_IMAGE` | *(required)* | `cmd/vcnode`, `examples/simple`, `scripts/demo.sh` | Digest-pinned image reference (`repo@sha256:<64hex>`). |
| `VC_IPFS_API` | `http://127.0.0.1:5001` | `pkg/freecompute`, `scripts/demo.sh` | Kubo HTTP RPC endpoint (must be loopback or Unix socket). |
| `VC_PODMAN_SOCKET` | `unix://${XDG_RUNTIME_DIR}/podman/podman.sock` | `pkg/freecompute`, `internal/cli`, `scripts/demo.sh` | Rootless Podman API socket endpoint. |
| `XDG_RUNTIME_DIR` | `/run/user/<uid>` | `internal/sandbox`, `scripts/demo.sh` | System directory for rootless user sockets. |
| `IPFS_API_ADDR` | `http://127.0.0.1:5001` | `internal/ipfs/integration_test.go` | Endpoint for `-tags integration` IPFS tests. |
| `SANDBOX_TEST_IMAGE` | `""` | `internal/sandbox/integration_test.go` | Image reference for `-tags integration` Podman tests. |

---

## Configuration Structs

### `freecompute.Config`

| Field | Type | Default | Validation Rule |
| :--- | :--- | :--- | :--- |
| `ID` | `string` | *(none)* | Required; non-empty node identifier. |
| `Root` | `string` | *(none)* | Required unless `WithChain` is used. |
| `Key` | `string` | *(none)* | Required unless `WithChain` is used (`<Key>.pub` must exist). |
| `Chain` | `string` | `"#vc"` | Chain alias (must begin with `#`, `$`, or `@`). |
| `Peers` | `[]string` | `nil` | Optional list of peer addresses synced on poll and post. |
| `StateDir` | `string` | `filepath.Join(Root, "vc-state")` | Target directory for task recovery files. |
| `PollInterval` | `time.Duration` | `500ms` | Chain polling interval (defaults to `500ms` if `<= 0`). |
| `TickAfter` | `time.Duration` | `10s` | Idle chain heartbeat interval (defaults to `10s` if `<= 0`). |

### `freecompute.WorkerPolicy`

| Field | Type | Default | Validation Rule |
| :--- | :--- | :--- | :--- |
| `AllowImages` | `[]string` | `nil` | All entries must pass `ParseImageRef` (`repo@sha256:...`). |
| `AllowAnyImage` | `bool` | `false` | When `false` and `AllowImages` is empty, all jobs are rejected. |
| `AllowOwnJobs` | `bool` | `false` | When `false`, jobs from the local public key are skipped. |
| `MaxConcurrent` | `int` | `1` | Max concurrent container executions (defaults to `1` if `<= 0`). |

### `freecompute.JobSpec`

| Field | Type | Default | Validation Rule |
| :--- | :--- | :--- | :--- |
| `Image` | `string` | *(none)* | Must match `repo@sha256:<64hex>` (no tags). |
| `Cmd` | `[]string` | *(none)* | Required execution vector; `len(Cmd) >= 1`. |
| `Input` | `Input` | *(none)* | Must be non-nil (`FromFile`, `FromDir`, `FromBytes`, `FromCID`). |
| `Replicas` | `uint32` | *(none)* | Worker commit count; `Replicas >= 1`. |
| `Quorum` | `uint32` | *(none)* | Minimum matching reveals; `1 <= Quorum <= Replicas`. |
| `Reward` | `int64` | `0` | Must be non-negative; requires sufficient Submitter reps. |
| `CommitWindow` | `uint32` | `20` (`DefaultCommitWindow`) | Chain positions after announce for commit acceptance. |
| `RevealWindow` | `uint32` | `20` (`DefaultRevealWindow`) | Chain positions after reveal phase opens. |

---

## Sandbox Isolation Model

Implemented in [`internal/sandbox/run.go`](../internal/sandbox/run.go) using Podman v5 API bindings:

| Setting | Podman `SpecGenerator` Parameter | Security Enforcement |
| :--- | :--- | :--- |
| Image reference | `ParseImageRef` | Strictly requires digest pinning (`@sha256:<64hex>`). |
| Unprivileged process | `s.User = "65534:65534"` | Executes as `nobody:nogroup`. |
| Read-only root | `s.ReadOnlyFilesystem = true` | Prevents writes outside mounted volumes. |
| Privilege elevation | `s.NoNewPrivileges = true`, `Privileged = false` | Disables setuid/setgid binary escalation. |
| Linux capabilities | `s.CapDrop = []string{"ALL"}` | Revokes all kernel capabilities. |
| Network isolation | `s.NetNS = Namespace{NSMode: NoNetwork}` | Disables all network interfaces. |
| `/input` mount | `ro,nodev,nosuid,noexec,rbind` | Exposes job inputs as read-only. |
| `/output` mount | `rw,nodev,nosuid,noexec,rbind` | Writable directory for results (`0777` host mode). |
| `/tmp` mount | `tmpfs (size=67108864,nodev,noexec,nosuid)` | 64 MiB RAM scratchpad. |
| Cgroup resource caps | `CPUs: 1.0`, `Memory: 256 MiB`, `PidsLimit: 100` | Prevents fork bombs and resource exhaustion. |
| Container tracking | `s.Labels["vc.managed"] = "true"` | Enables automatic orphaned container cleanup. |

---

## Ergo Actor Hierarchy

Processes run inside an Ergo actor node supervised by `NodeSupervisor` ([`internal/actors`](../internal/actors)):

```
NodeSupervisor (vc_supervisor, OneForOne)
├── ChainBridge (chain_bridge)
├── Dispatcher (dispatcher)
├── JobActor (job_<jobID>, dynamic per submitted job)
└── TaskActor (task_<jobID>, dynamic per worker job)
```

| Actor | Process Name | Responsibilities |
| :--- | :--- | :--- |
| `NodeSupervisor` | `vc_supervisor` | Supervises child actors (`act.SupervisorStrategyTransient`). |
| `ChainBridge` | `chain_bridge` | Sole interface to Freechains CLI; polls order, syncs peers, publishes envelopes, and votes. |
| `Dispatcher` | `dispatcher` | Applies protocol updates, manages container concurrency slots, and spawns `JobActor` / `TaskActor`. |
| `JobActor` | `job_<job_id>` | Coordinates job submission, emits SDK events, finalizes job, verifies output CID, and casts votes. |
| `TaskActor` | `task_<job_id>` | Runs container, atomically persists state in `--state-dir`, posts commit and reveal, and cleans state on finalize. |

---

## On-Disk Directory Layout

```text
<root>/
├── daemon.pid        # Process ID of running Freechains daemon
├── chains/           # Freechains DAG stores
└── vc-state/         # Worker persistence directory (--state-dir)
    └── <job_id>.json # Atomic state: {"job_id","nonce","result_hash","output_dir","exit_code"}
```

---

## Project Source Layout

```text
.
├── Makefile               # Build, test, race, and demo targets
├── go.mod / go.sum        # Go dependencies
├── proto/vc/v1/           # Protobuf definitions
├── vc/v1/                 # Generated Protobuf Go bindings
├── pkg/freecompute/       # Public Go SDK (Node, Submit, Serve, Fetch, Job)
├── cmd/
│   ├── vcnode/            # Production CLI daemon
│   └── vcnode-faulty/     # Fault-injection Worker binary (-tags faulty)
├── examples/simple/       # SDK usage example tool
├── internal/
│   ├── actors/            # Ergo supervision tree and actors
│   ├── canon/             # Canonical SHA-256 directory digest
│   ├── cli/               # Shared CLI flags and loop
│   ├── faultinject/       # Result tampering decorator (-tags faulty)
│   ├── ipfs/              # Kubo RPC client and safe directory fetch
│   ├── protocol/          # Pure deterministic protocol state machine
│   └── sandbox/           # Rootless Podman container runner
├── freechains/            # Go client wrapper for Freechains CLI
├── scripts/demo.sh        # 3-node end-to-end integration demo
└── docs/                  # Detailed documentation guides
```

