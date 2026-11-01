# Protocol Specification

This document details the deterministic state machine, message schemas, consensus window mechanics, and economic model of Freecompute.

## Architecture and Data Flow

Freecompute coordinates batch workloads across untrusted nodes using three decoupled layers:

1. **Control plane (Freechains)**: A peer-to-peer causal Directed Acyclic Graph (DAG) that enforces message ordering (`list order`), verifies Ed25519 cryptographic identities (`sign`), and maintains an on-chain reputation ledger (`reps`, `like`, `dislike`).
2. **Storage plane (Kubo IPFS)**: Off-chain content-addressed storage for `/input` and `/output` directory trees, keeping chain messages small.
3. **Execution sandbox (Rootless Podman)**: Network-isolated, capability-dropped container execution pinning image digests.

### Off-Chain Data Flow

1. **Input upload**: The Submitter packages job input files into a local temporary folder and invokes `Store.AddDir`. Single files (`FromFile`) or memory slices (`FromBytes`) are wrapped in a directory so containers always read from `/input/<filename>`.
2. **Deterministic IPFS parameters** ([`internal/ipfs/kubo.go`](../internal/ipfs/kubo.go)): Directory and file additions use CIDv1, `sha2-256`, `raw-leaves=true`, chunker `size-262144` (256 KiB), and `Pin=true`.
3. **Safe materialization** ([`internal/ipfs/fetch.go`](../internal/ipfs/fetch.go)): `Store.Fetch` enforces hard bounds: `MaxBytes = 10 MiB`, `MaxFiles = 100`, and `MaxDepth = 8`. Symlinks and non-regular DAG nodes are rejected with `ErrUnsafeContent`.
4. **Pinning lifecycle**: CIDs added locally remain pinned on the node's Kubo instance. Garbage collection (`ipfs repo gc`) and unpinning are managed by node operators out of band.

---

## Protobuf Schema

All chain envelopes are serialized with deterministic Protobuf options ([`proto/vc/v1/envelope.proto`](../proto/vc/v1/envelope.proto)):

| Message | Field | Type | Number | Description |
| :--- | :--- | :--- | :--- | :--- |
| `Envelope` | `version` | `uint32` | `1` | Protocol version (`1`). |
| `Envelope` | `job_id` | `string` | `2` | 64-character lowercase hex SHA-256 job identifier. |
| `Envelope` | `announce` | `JobAnnounce` | `10` | `oneof body`: announces a new job. |
| `Envelope` | `commit` | `ResultCommit` | `11` | `oneof body`: commits to a blinded output hash. |
| `Envelope` | `reveal` | `ResultReveal` | `12` | `oneof body`: reveals output hash, CID, nonce, and exit code. |
| `Envelope` | `finalize` | `JobFinalize` | `13` | `oneof body`: records the deterministic tally verdict. |
| `Envelope` | `tick` | `Tick` | `14` | `oneof body`: advances chain position on quiet chains. |
| `JobAnnounce` | `image_ref` | `string` | `1` | Digest-pinned container image (`repo@sha256:<64hex>`). |
| `JobAnnounce` | `cmd` | `repeated string` | `2` | Container execution vector (`argv`). |
| `JobAnnounce` | `input_cid` | `string` | `3` | IPFS CIDv1 of the `/input` directory tree. |
| `JobAnnounce` | `limits` | `ResourceLimits` | `4` | Schema-reserved resource limits message. |
| `JobAnnounce` | `replicas` | `uint32` | `5` | Required Worker commit count `N` (`>= 1`). |
| `JobAnnounce` | `quorum` | `uint32` | `6` | Minimum matching reveals required `Q` (`1 <= Q <= replicas`). |
| `JobAnnounce` | `reveal_window` | `uint32` | `7` | Max chain positions after reveal phase opens (`> 0`). |
| `JobAnnounce` | `reward_reps` | `uint64` | `8` | Reputation points transferred per vote. |
| `JobAnnounce` | `commit_window` | `uint32` | `9` | Max chain positions after announce for commits (`> 0`). |
| `ResourceLimits` | `cpus`, `memory_mb`, `timeout_sec`, `max_output_bytes` | `uint32` / `uint64` | `1..4` | Schema fields reserved for per-job limits. |
| `ResultCommit` | `commitment` | `bytes` | `1` | 32-byte SHA-256 commitment digest. |
| `ResultReveal` | `result_hash` | `bytes` | `1` | 32-byte SHA-256 canonical digest of `/output`. |
| `ResultReveal` | `result_cid` | `string` | `2` | IPFS CIDv1 of the `/output` directory. |
| `ResultReveal` | `nonce` | `bytes` | `3` | 32-byte random secret nonce. |
| `ResultReveal` | `exit_code` | `int32` | `4` | Container process exit status. |
| `JobFinalize` | `accepted_hash` | `bytes` | `1` | Winning 32-byte hash (empty if no quorum). |
| `JobFinalize` | `winners` | `repeated string` | `2` | Counted Worker public keys matching `accepted_hash`. |
| `JobFinalize` | `divergent` | `repeated string` | `3` | Counted Worker public keys with differing reveals (or all revealers if no quorum). |
| `JobFinalize` | `no_reveal` | `repeated string` | `4` | Counted Worker public keys failing to reveal. |
| `Tick` | `job_id` | `string` | `1` | Job ID target for heartbeat chain advancement. |

---

## Cryptographic Formulas

State functions are implemented in [`internal/protocol/protocol.go`](../internal/protocol/protocol.go):

- **Job ID**:
  $$\text{JobID} = \text{SHA-256}(\text{proto.MarshalOptions}\{\text{Deterministic: true}\}.\text{Marshal}(\text{JobAnnounce}))$$
- **Commitment Formula**:
  $$\text{commitment} = \text{SHA-256}(\text{job\_id} \parallel \text{0x00} \parallel \text{worker\_id} \parallel \text{0x00} \parallel \text{result\_hash} \parallel \text{nonce})$$
  Here `job_id` and `worker_id` are UTF-8 byte sequences separated by single `0x00` bytes to prevent concatenation collision. `result_hash` and `nonce` are 32-byte byte arrays.
- **Worker Identity Authenticity**: `worker_id` is never read from the payload. `ChainBridge` extracts the author public key (`ssh-ed25519 <base64>`) from Freechains action metadata (`sign`), ensuring unforgeable author attribution.

---

## Consensus Phases and Windows

Deadlines are evaluated strictly by chain position (`Entry.Index` in `freechains chain <alias> list order`), never wall-clock time:

1. **`Committing`**:
   - Opens at `AnnounceIndex` when `JobAnnounce` is accepted.
   - Distinct Workers post `ResultCommit` (up to `replicas`).
   - When `len(Commits) == replicas`, the phase immediately transitions to `Revealing` and sets `RevealOpenIndex = e.Index`.
   - If an entry arrives where `e.Index - AnnounceIndex > CommitWindow`: if `len(Commits) >= Quorum`, phase transitions to `Revealing` (`RevealOpenIndex = e.Index`); otherwise `Phase = Expired` and `Finalizable = true`.
2. **`Revealing`**:
   - Accepts `ResultReveal` from authors listed in `State.Commits` while `e.Index - RevealOpenIndex <= RevealWindow` and `!Finalizable`.
   - When all counted Workers reveal (`len(Reveals) == len(Commits)`), `Finalizable = true`.
   - When `e.Index - RevealOpenIndex > RevealWindow`, `Finalizable = true`.
   - If `Finalizable` and `!Verdict.HasQuorum`, `Phase = Expired`.
3. **`Tick` Heartbeats**:
   - Window expiration requires chain activity to advance `Entry.Index`.
   - Active nodes run a local timer `TickAfter` (default `10s`, capped at `MaxTicks = 50`). If no actions are observed for `TickAfter`, the node posts an empty `Tick{job_id}` envelope. The node pays the normal Freechains post fee.
4. **`Finalize` and Acceptance**:
   - Once `Finalizable == true`, any node may post `JobFinalize`.
   - The Submitter's `JobActor` posts immediately upon observing `Finalizable`. Worker `TaskActor` instances wait `5s` (`100ms` when `TickAfter < 1s`) before posting as a fallback if the Submitter is offline.
   - `VerifyFinalize` validates the tally against `state.Verdict`. The first valid `JobFinalize` in chain order locks `State.Finalize` and sets `Phase = Accepted` (or `Expired`).

---

## Quorum and Tally Rules

`Tally(commits, reveals, quorum)` executes deterministically:

1. Valid reveals are grouped by hex-encoded `result_hash` in order of appearance.
2. A group is eligible if `len(members) >= quorum`.
3. The eligible group with the largest membership wins. Ties preserve the first group to reach that size.
4. **When quorum is met**:
   - `HasQuorum = true`, `AcceptedHash = group.hash`, `WinnerCID = group.cid` (from first revealer).
   - In commit order: revealers matching `AcceptedHash` become `Winners`; revealers with different hashes become `Divergent`; authors without a reveal become `NoReveal`.
5. **When quorum is not met**:
   - `HasQuorum = false`, `AcceptedHash = nil`, `WinnerCID = ""`.
   - In commit order: all revealers become `Divergent`; non-revealers become `NoReveal`.

---

## Ignored Messages

`Apply` ignores invalid entries silently without breaking chain replay:

- Entries with non-monotonic `Index <= state.LastIndex`, missing `Author`, or `nil` envelope.
- Messages arriving before a valid `JobAnnounce` or referencing an mismatched `job_id`.
- `JobAnnounce` with invalid bounds (`replicas < 1`, `quorum < 1`, `quorum > replicas`, or zero windows).
- `ResultCommit` arriving outside `Committing`, with `len(commitment) != 32`, from duplicate authors, or after `replicas` slots are filled.
- `ResultReveal` arriving outside `Revealing`, after window expiration, from uncommitted authors, with invalid lengths, or failing `Commitment(...)` equality.
- `JobFinalize` when `!VerifyFinalize(state, finalize)` or if finalization was already applied.
- Any message arriving after `state.Finalize != nil`.

---

## Why Commit-Reveal?

A direct-reveal protocol allows freeloaders to monitor the chain, copy honest results, and claim rewards without executing containers.

Commit-reveal neutralizes freeloading through two properties:

1. **Hiding (`nonce`)**: Workers publish only a 32-byte commitment. Because `nonce` contains 32 random bytes, peers cannot determine `result_hash` before `Revealing` opens.
2. **Identity binding (`worker_id`)**: The commitment formula includes the author's public key. If Worker B copies Worker A's commitment, Worker B's later reveal will compute `SHA-256(... || worker_B || ... || result_hash || nonce) != commitment_A` and fail verification. Worker B is placed in `NoReveal` and penalized with a `dislike`.

---

## Deterministic Directory Digest

Workload validity requires byte-for-byte identical output directories across diverse host kernels.

[`internal/canon/canon.go`](../internal/canon/canon.go) defines `DirectoryDigest(root, Limits{MaxBytes: 10 MiB, MaxFiles: 100})`:

- Walks `root` using `os.Lstat`. Symlinks, devices, sockets, and FIFOs are rejected immediately (`ErrUnsafeEntry`).
- Collects all regular files, standardizing relative paths with forward slashes (`/`).
- Sorts file paths in lexicographic byte order.
- Feeds regular file entries sequentially into SHA-256:
  ```text
  <relPath>\n<size_decimal>\n<raw_file_bytes>\n
  ```
- File permissions (`mode`), UID/GID attributes, and modification timestamps (`mtime`) are discarded.
- An empty directory digests to `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855` (SHA-256 of zero bytes).

---

## Reputation Ledger Mechanics

Freechains reputation rules govern resource usage:

- **Pioneers**: The chain is initialized with pioneer keys dividing `50000` initial reputation points equally (`16666` each for 3 pioneers).
- **Post deposit and causal refund**: Posting requires at least `1` rep and locks `500` reps (`DefaultPostCost = 500`). The deposit is refunded once a causal descendant signed by another peer references the post. Unbacked posts at the DAG tip temporarily show `-500` reps.
- **Vote economics and 10% burn**: A `like` or `dislike` of `R` points deducts `R` from the Submitter, burns `10%` (`0.1 * R`), and awards `+0.9 * R` (`like`) or penalizes `-0.9 * R` (`dislike`) to the target author. For default `reward = 1000`, the Submitter spends `1000` reps and the recipient's balance changes by `±900`.
- **Submitter pre-check**: `Node.Submit` checks that:
  $$\text{reps}_{\text{submitter}} \ge \text{Reward} \times \text{Replicas} + 2 \times \text{PostCost}$$
  For defaults (`1000 * 3 + 2 * 500 = 4000`), Submitters holding fewer than `4000` reps fail immediately with `ErrInsufficientReps`.
- **Self-voting exclusion**: In `JobActor.vote`, if `author == SubmitterPublicKey`, no vote is posted.

