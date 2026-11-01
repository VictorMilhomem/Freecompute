# End-to-End Demo Guide (`scripts/demo.sh`)

This guide explains the local multi-node demonstration script [`scripts/demo.sh`](../scripts/demo.sh), including execution traces, output interpretation, reputation accounting, and fault-injection scenarios.

---

## Script Architecture and Execution Flow

`scripts/demo.sh` orchestrates three independent Freechains nodes (`alice`, `bob`, `carol`) running on a single host machine:

1. **Pre-flight verification**: Checks required system binaries (`freechains`, `ipfs`, `podman`, `ssh-keygen`, `go`, `curl`, `flock`), verifies that `VC_IMAGE` is set to a digest-pinned reference, and tests connectivity to the local Kubo API and rootless Podman Unix socket.
2. **Workspace preparation**: Creates an isolated directory under `/tmp/vc-demo-XXXXXX`. Freechains invocations use a per-root file lock (`.fc.lock`) via `flock` to serialize concurrent CLI commands.
3. **Binary compilation**: Verifies that `bin/vcnode`, `bin/example-simple`, and (in divergent mode) `bin/vcnode-faulty` are up to date.
4. **Key generation and daemon startup**: Generates Ed25519 keypairs for `alice`, `bob`, and `carol`, and launches three Freechains daemons on ports `8331`, `8332`, and `8333` with `--hub` and `--reuseaddr`.
5. **Chain genesis**: Initializes chain `#vc` on `alice` with all three nodes as pioneers (allocating `16666` reps to each from the `50000` genesis pool), then clones the chain to `bob` and `carol`.
6. **Workload setup**: Writes `/tmp/vc-demo-XXXXXX/data.txt` containing 11 words:
   ```text
   volunteer computing on freechains ipfs and podman works end to end
   ```
7. **Worker startup**:
   - `bob`: Honest Worker running `bin/vcnode run --allow-any-image`.
   - `carol`: Honest Worker running `bin/vcnode run` in `make demo`, or fault-injected Worker running `bin/vcnode-faulty` in `make demo-divergent`.
8. **Submitter execution**: Runs `alice` using the SDK example tool (`bin/example-simple --allow-own-jobs --image="$VC_IMAGE"`). `--allow-own-jobs` allows `alice` to act as both Submitter and the 3rd Worker.
9. **Verification**: Compares output hashes, verifies that `alice` retrieved and confirmed the matching result from IPFS, checks reputation balances, and asserts expected pass criteria.

---

## Honest Demo Execution (`make demo`)

```bash
make demo
```

### Complete Output Trace

```text
CGO_ENABLED=0 go build -o bin/vcnode ./cmd/vcnode
CGO_ENABLED=0 go build -o bin/example-simple ./examples/simple
./scripts/demo.sh
==> Checking binaries...
==> Generating ed25519 keys for alice, bob, carol...
==> Starting three Freechains daemons (8331, 8332, 8333) with --hub...
==> Initializing #vc on alice with three pioneers and cloning to bob and carol...
    alice chain id: #5493dc2d17d69f8b22b378faf13e2c73518e3c24
    bob   chain id: #5493dc2d17d69f8b22b378faf13e2c73518e3c24
    carol chain id: #5493dc2d17d69f8b22b378faf13e2c73518e3c24
==> Reputation (before):
    alice: 16666
    bob:   16666
    carol: 16666
==> Starting worker nodes bob (honest) and carol (honest)...
==> Starting alice via bin/example-simple (SDK Submitter)...

==> Chain Timeline (consensus order on alice):
    [0] 50b7f3f96d4ec578be0fbc43199c288e06d5bfb2  kind=post    author=alice
    [1] 57484f90c263665b5c952dd4c3a6fc4236e01dae  kind=post    author=bob
    [2] 308407e63d2f77b2ca94d2a5e68173db141852b0  kind=post    author=alice
    [3] 9cf925551c4f3caa838d9b340b497be9b213d827  kind=post    author=carol
    [4] e06e68225df9a9d5ec819ef38b0a1d597b37fcaf  kind=post    author=alice
    [5] facfe8359f007f3d78c41996b1faa2b35fa238cd  kind=post    author=carol
    [6] 67a0ecc61dd6d8396039d11d8f6f2e77fe92c529  kind=post    author=bob
    [7] 3f1ed78f379fd6b77006dbb979e4e7d1db4a3db9  kind=post    author=alice
    [8] 6bb93bc075eafccd4471a8dac0ef7c0c460c55f6  kind=like    author=alice
    [9] e27ba6543eee148ea4a3a6ab33c22eb107592a40  kind=like    author=alice

==> Submitter Report & Verdict:
JOB_ID=4b80fbeb9e64c37ee6b5dd15ad3fecad763d757156e3f9a3a7759061c4cb3f15
ACCEPTED_HASH=d3beb797b44ba88fdceea6cef75d697cfc0b8285dc682f706c42efbcaa756622
VERIFIED=true
RESULT=11
WINNERS=ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAvst2Fv4K8tQdHy9+m7spR8ShSDKoahXpjKm7w+Jm2N,ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM8x7T10x3NbrZy5yQi1wU/qafGlLoodglrOBpRpaBrb,ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIMIIe8SeHNCZvtNApRgp5iQESk30emhfDqCUx9HAvOMs
DIVERGENT=
NO_REVEAL=
    WINNERS_ALIASES=bob,alice,carol
    DIVERGENT_ALIASES=

==> Reputation (after):
    alice: 14166 (before: 16666)
    bob:   17066 (before: 16666)
    carol: 17566 (before: 16666)

PASS
```

---

## Divergent Demo Execution (`make demo-divergent`)

In divergent mode, `carol` executes `bin/vcnode-faulty` (compiled with `-tags faulty`), which modifies `/output/result.txt` to `999\n` before computing the directory hash:

```bash
make demo-divergent
```

### Complete Output Trace

```text
CGO_ENABLED=0 go build -o bin/vcnode ./cmd/vcnode
CGO_ENABLED=0 go build -tags faulty,containers_image_openpgp,exclude_graphdriver_btrfs -o bin/vcnode-faulty ./cmd/vcnode-faulty
CGO_ENABLED=0 go build -o bin/example-simple ./examples/simple
./scripts/demo.sh --divergent
==> Checking binaries...
==> Generating ed25519 keys for alice, bob, carol...
==> Starting three Freechains daemons (8331, 8332, 8333) with --hub...
==> Initializing #vc on alice with three pioneers and cloning to bob and carol...
    alice chain id: #19e74b2a64c189f4821ae8395ca031e96e7a2c24
    bob   chain id: #19e74b2a64c189f4821ae8395ca031e96e7a2c24
    carol chain id: #19e74b2a64c189f4821ae8395ca031e96e7a2c24
==> Reputation (before):
    alice: 16666
    bob:   16666
    carol: 16666
==> Starting worker nodes bob (honest) and carol (vcnode-faulty)...
==> Starting alice via bin/example-simple (SDK Submitter)...

==> Chain Timeline (consensus order on alice):
    [0] 95c5f36e5a44f8c14be75cc39c0cad0f62b86edd  kind=post    author=alice
    [1] eb0771238976ee96b5eea28781752ebd4d838464  kind=post    author=bob
    [2] 828a8e849d6fb905eb97058c9e6e5a0c92fccc58  kind=post    author=alice
    [3] 5f7f8a2d03a4c74d660b3ec5fa3079e49c40885d  kind=post    author=carol
    [4] 270215398e2de76a2a977f903366a93d1162e4de  kind=post    author=alice
    [5] c0815cd5014d797eb1e1f43c0d5dcc4e274a5e70  kind=post    author=bob
    [6] 2db88f37d487de70b2bf43ea054de9b64368b043  kind=post    author=carol
    [7] 3436c6c05cf410e2e065a26813352cbb3bb618c8  kind=post    author=alice
    [8] 807cccd35333f28ba90d95945a207dde11e75547  kind=like    author=alice
    [9] b344127d098b10efdc016c44bc3b3562d5ef545f  kind=post    author=carol
    [10] 59eaac5ce5ae1332908415b5486fa3c324889f38  kind=like    author=alice

==> Submitter Report & Verdict:
JOB_ID=4b80fbeb9e64c37ee6b5dd15ad3fecad763d757156e3f9a3a7759061c4cb3f15
ACCEPTED_HASH=d3beb797b44ba88fdceea6cef75d697cfc0b8285dc682f706c42efbcaa756622
VERIFIED=true
RESULT=11
WINNERS=ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIEsLeSYNJ7cDzXFxB2SddJC6mH+BSXCmUriHhOQRPwPY,ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIPsBX2SGzWp3S3xHvpo2aay9QREEXkgwRTzTazFk3VpY
DIVERGENT=ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINjZxFquNQlIdZ9xJWqnHpJCmar3ukrNzbxEsOL7IBbS
NO_REVEAL=
    WINNERS_ALIASES=bob,alice
    DIVERGENT_ALIASES=carol

==> Reputation (after):
    alice: 14666 (before: 16666)
    bob:   17566 (before: 16666)
    carol: 15266 (before: 16666)

PASS
```

---

## Output Interpretation

- **Chain Timeline**:
  - `[0]`: `alice` broadcasts `JobAnnounce`.
  - `[1]..[3]`: Three distinct Workers post `ResultCommit`.
  - `[4]..[6]`: Three Workers post `ResultReveal`.
  - `[7]`: Submitter posts `JobFinalize`.
  - `[8]`, `[9]` (or `[10]`): Submitter casts `like` and `dislike` votes. Freechains metadata records both under `action like` with positive or negative values.
- **Report Parameters**:
  - `JOB_ID`: Deterministic SHA-256 of `JobAnnounce`.
  - `ACCEPTED_HASH`: Winning directory digest.
  - `VERIFIED`: `true` when Submitter fetched the CID from IPFS and verified the hash matches.
  - `RESULT`: Value extracted from `result.txt` (`11`).
  - `WINNERS` / `DIVERGENT` / `NO_REVEAL`: Grouping of public keys according to reveal matches.

---

## Reputation Economics Breakdown

Initial balances: `alice = 16666`, `bob = 16666`, `carol = 16666`.

- **Alice (Submitter)**:
  - Spends `1000` reps per vote to external workers. In both scenarios, `alice` votes on `bob` and `carol` (`-2000`).
  - Self-votes are skipped (`alice` does not vote on herself).
  - Unbacked posts at the DAG tip temporarily lock `500` reps as post deposits.
- **Bob (Honest Worker)**:
  - Receives `like` of `1000` reps. With 10% burned, net gain is `+900` (`17566`). If the reveal post remains temporarily unbacked at the tip, balance shows `17066`.
- **Carol (Divergent Worker)**:
  - Receives `dislike` of `1000` reps. With 10% burned, net change is `-900` (`15766`).
  - In divergent runs, `carol` posts a fallback `JobFinalize` (`[9]`), which locks `500` reps unbacked, resulting in `15266`.

---

## Inspecting Logs and State

To retain demo artifacts after execution, comment out the `rm -rf "$TMP_DIR"` line in `scripts/demo.sh`:

- `/tmp/vc-demo-XXXXXX/alice.log`: Structured JSON logs and actor events from the Submitter.
- `/tmp/vc-demo-XXXXXX/bob/vcnode.log`: Worker daemon logs from `bob`.
- `/tmp/vc-demo-XXXXXX/alice-state/`: Worker crash-recovery files (`<job_id>.json`).
- `/tmp/vc-demo-XXXXXX/alice-out/`: Directory containing fetched winning results.

