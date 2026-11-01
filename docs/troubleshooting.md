# Troubleshooting Guide

This guide covers common runtime errors, configuration mismatches, and recovery procedures across Freecompute nodes, Freechains daemons, Kubo IPFS, and rootless Podman.

---

## Issue Resolution Matrix

| Symptom | Cause | Remediation |
| :--- | :--- | :--- |
| `IPFS daemon unreachable` or `MISSING running Kubo API at http://127.0.0.1:5001` | Local Kubo daemon is inactive or listening on an alternate port. | Start the daemon via `ipfs daemon` (run `ipfs init` first if uninitialized), or set `VC_IPFS_API=http://127.0.0.1:<port>`. |
| `MISSING Podman socket` or `Podman service unreachable` | The rootless Podman API socket service is inactive. | Start the socket via `systemctl --user start podman.socket` or foreground service `podman system service --time=0 &`. |
| `image reference ... contains tag` or `missing @sha256: digest` | Image reference provided in `VC_IMAGE` or `JobSpec.Image` contains a mutable tag (`alpine:3.20`) rather than a digest. | Inspect the repository digest: `podman image inspect <img> --format '{{index .RepoDigests 0}}'` and supply `repo@sha256:<64hex>`. |
| Container fails with `Permission denied` creating files in `/output` | Host output directory lacked world-writable permissions for rootless mapped UID `65534:65534`. | Ensure the output directory is created with mode `0777` (handled automatically by `TaskActor.runContainer`). |
| Container fails with `Permission denied` accessing `/input` or `/output` under SELinux | SELinux enforces strict confinement on container volume mounts under `/tmp`. | Ensure the directory is on a filesystem supporting extended attributes so `SandboxAdapter` can assign `container_file_t`, or label manually: `chcon -Rt container_file_t <dir>`. |
| `freecompute: insufficient reputation: have X reps, need Y` | Submitter balance is below the required threshold: `Reward * Replicas + 2 * PostCost`. | Lower `Reward` or `Replicas`, wait for pending posts to be causally backed, or transfer reps from an existing pioneer key. |
| `FAIL: chain IDs do not match` or nodes do not exchange envelopes | Chains were initialized independently with different pioneer sets rather than cloned. | Initialize `#vc` on one node (`chains add #vc init --pioneer=...`) and clone on all others (`chains add #vc clone <host:port>`). |
| `sync send` / `sync recv` fails or DAG actions do not propagate | Freechains daemons were launched without P2P hub routing enabled. | Launch daemons with hub routing: `freechains --root=<dir> daemon start --port=<port> --hub -- --reuseaddr`. |
| Job stays in `Committing` or `Revealing` and never finalizes | Fewer than `Quorum` Workers accept the job (e.g. image rejected by `AllowImages`, or Submitter is one of 3 nodes without `--allow-own-jobs`). | Check Worker logs for `image not allowed` or `ignoring own job announce`; ensure at least `Q` workers allow the image and peer connectivity is intact. |
| Worker process crashes or restarts after posting `ResultCommit` | Host restart or process termination during the execution or commit window. | Relaunch using the identical `--state-dir` (default `<root>/vc-state`). `TaskActor` reloads state, avoids duplicate commits, and reveals using saved nonces. |
| Freechains daemon fails to bind port `8331`/`8332`/`8333` (`address already in use`) | Residual Freechains daemons from previous runs remain bound to the ports. | Terminate stale processes (`pkill -f 'freechains.*daemon'`) and remove temporary socket directories. |
| Leftover containers from an abruptly killed `vcnode` process (`SIGKILL`) | Process terminated before deferred container cleanup routines could run. | Start `vcnode` (which automatically runs `Runner.Cleanup` on startup) or remove manually: `podman rm -f --filter label=vc.managed=true`. |

