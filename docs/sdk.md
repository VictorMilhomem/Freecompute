# Freecompute Go SDK Guide (`pkg/freecompute`)

The public Go SDK provides programmatic access to Freecompute nodes, job submission, execution policies, and event streams.

> **API Status (`v0`)**: `pkg/freecompute` is an experimental `v0` API. Types, options, and signatures may change across iterations.

---

## Public API Reference

Import path: `github.com/VictorMilhomem/freecompute/pkg/freecompute`

### Core Types and Methods

| Symbol | Kind | Description |
| :--- | :--- | :--- |
| `Config` | `struct` | Configuration for node identity, Freechains paths, and timers. |
| `NewNode(cfg, opts...)` | `func` | Validates configuration and initializes a `*Node`. |
| `(*Node).Start(ctx)` | `method` | Validates IPFS and Podman connectivity, cleans orphaned containers, and starts Ergo actors. |
| `(*Node).Stop(ctx)` | `method` | Stops dispatcher, cancels active containers, resolves pending jobs with `ErrNodeStopped`, and halts Ergo. |
| `(*Node).Submit(ctx, spec)` | `method` | Validates Submitter reps, uploads input to IPFS, broadcasts announce, and returns a `*Job`. |
| `(*Node).Serve(policy)` | `method` | Enables Worker mode under the provided `WorkerPolicy`. Can be called before or after `Start`. |
| `(*Node).Fetch(ctx, cid, dest)` | `method` | Downloads an IPFS directory CID safely into empty host directory `dest`. |
| `JobSpec` | `struct` | Definition of a job: `Image`, `Cmd`, `Input`, `Replicas`, `Quorum`, `Reward`, `CommitWindow`, `RevealWindow`. |
| `Job` | `struct` | Active job handle exposing `ID()`, `Events()`, `DroppedEvents()`, and `Wait(ctx)`. |
| `Result` | `struct` | Job outcome: `JobID`, `AcceptedHash`, `OutputCID`, `Winners`, `Divergent`, `NoReveal`. |
| `WorkerPolicy` | `struct` | Execution constraints: `AllowImages`, `AllowAnyImage`, `AllowOwnJobs`, `MaxConcurrent`. |

### Input Constructors

- `FromFile(path string) Input`: Wraps a host file into an input directory mounted at `/input/<basename>`.
- `FromDir(path string) Input`: Uses an existing host directory tree as `/input`.
- `FromBytes(name string, data []byte) Input`: Mounts an in-memory byte slice as `/input/<name>`.
- `FromCID(cid string) Input`: Reuses an existing IPFS CIDv1 directory tree.

### Functional Options

- `WithStore(store ipfs.Store)`: Injects a custom or in-memory IPFS store.
- `WithRunner(runner sandbox.Runner)`: Injects a custom or in-memory container runner.
- `WithChain(chain ChainAPI)`: Injects a custom or in-memory Freechains chain client.
- `WithLogger(logger *slog.Logger)`: Sets custom structured logging.
- `WithPostCost(cost int64)`: Overrides default Freechains post cost (`500`).
- `WithPublicKey(pub string)`: Explicitly sets author public key (`ssh-ed25519 ...`).
- `WithMaxTicks(max uint32)`: Caps maximum heartbeat ticks posted per job (`50`).

### Events and Sentinel Errors

Events delivered on `Job.Events()`:
- `Announced`, `Committed`, `RevealPhaseOpened`, `Revealed`, `Finalized`, `Expired`.

Sentinel errors testable via `errors.Is`:
| Sentinel Error | Cause |
| :--- | :--- |
| `ErrInvalidSpec` | Unpinned image, empty command, missing paths, invalid replicas/quorum, or unstarted node. |
| `ErrInsufficientReps` | Submitter reps are less than `Reward * Replicas + 2 * PostCost`. |
| `ErrJobExpired` | Commit or reveal window elapsed without reaching quorum. |
| `ErrNoQuorum` | Wrapped inside `ErrJobExpired` (`fmt.Errorf("%w: %w", ErrJobExpired, ErrNoQuorum)`). |
| `ErrImageNotAllowed` | Container image rejected by the Worker allowlist. |
| `ErrNodeStopped` | Operation called on a stopped node, or in-flight job interrupted by `Node.Stop`. |

---

## SDK Usage Patterns

### Submitter: Submit and Wait

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/VictorMilhomem/freecompute/pkg/freecompute"
)

func main() {
	ctx := context.Background()
	node, err := freecompute.NewNode(freecompute.Config{
		ID:    "alice",
		Root:  "/tmp/alice/root",
		Key:   "/tmp/keys/alice",
		Chain: "#vc",
		Peers: []string{"localhost:8332", "localhost:8333"},
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := node.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer node.Stop(context.Background())

	job, err := node.Submit(ctx, freecompute.JobSpec{
		Image:    os.Getenv("VC_IMAGE"),
		Cmd:      []string{"sh", "-c", "wc -w < /input/data.txt > /output/result.txt"},
		Input:    freecompute.FromBytes("data.txt", []byte("volunteer computing\n")),
		Replicas: 3,
		Quorum:   2,
		Reward:   1000,
	})
	if err != nil {
		log.Fatal(err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	res, err := job.Wait(waitCtx)
	if err != nil {
		log.Fatal(err)
	}

	outDir, _ := os.MkdirTemp("", "vc-res-*")
	defer os.RemoveAll(outDir)

	if err := node.Fetch(ctx, res.OutputCID, outDir); err != nil {
		log.Fatal(err)
	}

	content, _ := os.ReadFile(filepath.Join(outDir, "result.txt"))
	fmt.Printf("Job %s completed. Result: %s\n", res.JobID, string(content))
}
```

### Worker: Serving Workloads

```go
policy := freecompute.WorkerPolicy{
	AllowImages:   []string{os.Getenv("VC_IMAGE")},
	AllowOwnJobs:  false,
	MaxConcurrent: 2,
}
node.Serve(policy)
```

### Observing Event Streams

`Job.Events()` provides a buffered (`64`) non-blocking channel:

```go
go func() {
	for ev := range job.Events() {
		switch e := ev.(type) {
		case freecompute.Committed:
			fmt.Printf("Worker committed: %s\n", e.Author)
		case freecompute.Revealed:
			fmt.Printf("Worker revealed: %s (hash=%x)\n", e.Author, e.ResultHash)
		case freecompute.Finalized:
			fmt.Printf("Job finalized with %d winners\n", len(e.Winners))
		}
	}
}()
```

If the reader falls behind, the oldest unread event is discarded, and `job.DroppedEvents()` increments.

### Unit Testing with In-Memory Fakes

Inject in-memory adapters without spawning external daemons:

```go
node, err := freecompute.NewNode(
	freecompute.Config{ID: "test-node", Chain: "#vc"},
	freecompute.WithChain(fakeChain),
	freecompute.WithStore(fakeStore),
	freecompute.WithRunner(fakeRunner),
	freecompute.WithPublicKey("ssh-ed25519 AAA..."),
)
```

---

## Writing Custom Workloads

Deterministic container requirements:
1. **Container image**: Must be pinned by digest (`repo@sha256:<64hex>`).
2. **Filesystem conventions**:
   - `/input`: Mounted read-only. Read inputs from here.
   - `/tmp`: Memory-backed scratchpad (`64 MiB`, `noexec`).
   - `/output`: Target directory. Write final regular files here (max `10 MiB`, max `100` files).
3. **Purity**: Avoid timestamps, random IDs, or unseeded algorithms in `/output`.

---

## SDK Example Tool (`examples/simple`)

The repository includes a reference binary in `examples/simple/main.go` built via `make build-example`:

```bash
bin/example-simple [flags]
```

### CLI Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--id` | *(required)* | Node alias identifier. |
| `--root` | *(required)* | Freechains root directory path. |
| `--key` | *(required)* | Private key path (`ed25519`; `<key>.pub` must exist). |
| `--chain` | `#vc` | Freechains chain alias. |
| `--peers` | `""` | Comma-separated `host:port` peer list. |
| `--state-dir` | `""` (`<root>/vc-state`) | Worker state persistence directory. |
| `--image` | `env VC_IMAGE` | Digest-pinned container image (`repo@sha256:...`). |
| `--input` | *(required)* | Input file path (mounted at `/input/<basename>`). |
| `--out` | `""` (temp dir) | Directory where the winning result is fetched. |
| `--replicas` | `3` | Worker count `N`. |
| `--quorum` | `2` | Minimum matching reveals `Q`. |
| `--reward` | `1000` | Reputation reward per vote. |
| `--commit-window` | `20` | Commit window in chain positions. |
| `--reveal-window` | `20` | Reveal window in chain positions. |
| `--allow-own-jobs` | `false` | Enable Worker mode for own jobs alongside Submitter. |
| `--timeout` | `3m` | Context timeout for `job.Wait`. |

