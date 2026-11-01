# Security and Trust Model

This document outlines the threat model, mitigation boundaries, operational requirements, and known security limitations of Freecompute.

---

## Mitigations

### 1. Freeloading and Result Copying
- **Threat**: An untrusted worker listens to peer reveals, copies another worker's output digest, and submits it to claim rewards without executing the container.
- **Mitigation**: Commit-reveal with secret nonces and identity binding. A worker commits to `SHA-256(job_id || 0x00 || worker_id || 0x00 || result_hash || nonce)`. Copying another worker's commitment fails verification at reveal time because `worker_id` is derived from the authenticated Freechains signature.

### 2. Identity Spoofing
- **Threat**: A node broadcasts messages claiming to be another worker.
- **Mitigation**: Author identities (`ssh-ed25519 ...`) are verified cryptographically by Freechains at the DAG layer. `ChainBridge` extracts `Entry.Author` solely from the `sign` action metadata, completely ignoring any identity claimed inside message bodies.

### 3. Sybil and Spam Attacks
- **Threat**: An attacker creates thousands of disposable public keys to flood commit slots or stall execution.
- **Mitigation**: Freechains requires positive reputation to post. Every post locks a `500`-rep deposit until causally backed by another node's signed post. Zero-rep keys cannot post.

### 4. Container Escape and Host Compromise
- **Threat**: A malicious workload exploits container privileges or network access to attack the host or local network.
- **Mitigation**:
  - Rootless Podman execution (`s.User = "65534:65534"`).
  - All capabilities dropped (`s.CapDrop = ["ALL"]`).
  - Read-only root filesystem (`s.ReadOnlyFilesystem = true`).
  - Strict network isolation (`s.NetNS = Namespace{NSMode: NoNetwork}`).
  - Privilege escalation disabled (`s.NoNewPrivileges = true`).
  - Bounded memory-backed scratchpad (`/tmp` tmpfs capped at 64 MiB, `noexec`).

### 5. Host File Exfiltration via Symlinks
- **Threat**: A container writes a symlink in `/output` pointing to `/etc/shadow` or host files, hoping the host daemon will read and publish them.
- **Mitigation**: Both `sandbox.VerifyOutputDir` and `canon.DirectoryDigest` inspect every output file using `os.Lstat`. If any entry is a symlink, FIFO, socket, or device node, execution immediately halts with `ErrUnsafeOutput` / `ErrUnsafeEntry`.

### 6. Resource Exhaustion (Zip-Bombs / Floods)
- **Threat**: Oversized payloads exhaust host disk space or memory.
- **Mitigation**: `ipfs.Fetch`, `sandbox.VerifyOutputDir`, and `canon.DirectoryDigest` enforce uniform hard bounds: max 10 MiB total data, max 100 regular files, and maximum directory depth of 8.

---

## Known Limitations and Open Challenges

1. **Worker Collusion**:
   If $Q$ or more workers collude off-chain, they can agree on an identical fabricated `result_hash` and `result_cid` and claim the quorum reward without running the container.
2. **Reputation Concentration**:
   Entities controlling multiple pioneer keys or high reputation balances can dominate commit slots across public chains.
3. **Data Confidentiality**:
   All inputs, outputs, commands, and image references are public on Kubo IPFS and the Freechains DAG. Private or sensitive data must not be processed on public chains without external encryption layers.
4. **Workload Determinism**:
   Workloads must produce identical bit-for-bit outputs. If an application outputs timestamps, random numbers, or non-deterministic file orderings, quorum will fail and workers may be penalized.
5. **Submitter Defaulting**:
   Reputation votes (`like` / `dislike`) are cast by the Submitter after `JobFinalize`. If a Submitter node crashes permanently before voting, workers receive no reputation transfers despite finishing the job.

---

## Operational Guidelines

1. **Strict Image Allowlisting**: Always maintain an explicit image allowlist (`AllowImages` / `--allow-image`) in production. Avoid `AllowAnyImage` outside isolated test environments.
2. **Rootless Podman**: Ensure Podman is executed under an unprivileged user namespace with standard cgroup v2 limits active.
3. **Loopback IPFS RPC**: Bind Kubo's API endpoint strictly to `127.0.0.1:5001`. `internal/ipfs` rejects non-loopback connections unless explicitly overridden with `AllowRemote: true`.

---

## Threat Matrix

| Threat | Defense | Severity | Resolution Status |
| :--- | :--- | :--- | :--- |
| Freeloading (result copying) | SHA-256 commit-reveal with 32-byte nonce and author key binding | High | Fully mitigated |
| Key spoofing | Freechains Ed25519 causal signature verification | High | Fully mitigated |
| Sybil spam | Freechains post deposit (500 reps) and reputation gates | High | Fully mitigated |
| Container escape | Rootless Podman, unprivileged UID, `CapDrop=ALL`, `NoNetwork` | Critical | Fully mitigated |
| Host symlink exfiltration | `os.Lstat` verification rejecting symlinks and special files | High | Fully mitigated |
| Storage exhaustion | 10 MiB / 100 files / depth 8 limits across fetch and digest | Medium | Fully mitigated |
| Worker restart state loss | Atomic `fsync` persistence in `--state-dir` before commit | Medium | Fully mitigated |
| Quorum collusion ($Q$ workers) | Quorum requires multiple keys, but slots are open-race | High | Unresolved |
| Submitter exit before voting | Upfront balance validation, but no on-chain smart contract | Medium | Unresolved |
| Payload eavesdropping | None (IPFS content and chain DAG are public) | Low | By design (public MVP) |

