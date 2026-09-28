# Freechains Go Library

`freechains` is a standalone Go module wrapping the Freechains v0.20 command-line interface behind a typed, testable API.

## Features

- **Ports & Adapters Architecture**: Core operations depend only on the `Runner` interface, enabling easy mocking and testing without requiring the `freechains` binary.
- **Typed Models & Strict Validation**: Dedicated types for `Alias`, `ActionID`, `PublicKey`, `PrivateKeyPath`, `ChainID`, and `Payload` with guards against option injection.
- **Serialized Execution**: Optional `SerialRunner` decorator to guard against concurrent invocations of the CLI.
- **Typed Error Classification**: Substring-based error classification mapping stderr output to sentinel errors (`ErrInsufficientReps`, `ErrPayloadRevoked`, `ErrActionNotFound`, etc.).
- **Asynchronous Daemon Management**: Non-blocking `Daemon` lifecycle controller.

## Usage

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/VictorMilhomem/freecompute/freechains"
)

func main() {
	ctx := context.Background()

	// 1. Setup runner and client
	runner := freechains.NewExecRunner()
	// Optionally serialize CLI calls:
	serialRunner := freechains.NewSerialRunner(runner)

	client, err := freechains.NewClient(serialRunner, freechains.Config{
		Binary: "freechains",
		Root:   "/tmp/my-freechains",
	})
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	// 2. Initialize a chain
	alias := freechains.Alias("#chat")
	chainID, err := client.ChainsAddInit(ctx, alias)
	if err != nil {
		log.Fatalf("failed to init chain: %v", err)
	}
	fmt.Printf("Chain created with ID: %s\n", chainID)

	// 3. Post an inline message with --beg
	postID, err := client.Post(ctx, alias, freechains.InlinePayload("Hello Freechains!"), freechains.PostOptions{
		Beg: true,
	})
	if err != nil {
		log.Fatalf("failed to post: %v", err)
	}
	fmt.Printf("Posted action: %s\n", postID)

	// 4. Query consensus order
	entries, err := client.ListOrder(ctx, alias)
	if err != nil {
		log.Fatalf("failed to list order: %v", err)
	}
	for _, entry := range entries {
		fmt.Printf("Action %s (revoked: %v)\n", entry.ID, entry.Revoked)
	}
}
```

## Assumptions

The Freechains CLI v0.20 documentation leaves certain output details unspecified. This library operates under the following explicit assumptions:

1. **Option Placement**: Global options (`--root=<dir>` and `--now=<secs>`) are placed immediately following the binary name in the argument slice (e.g. `freechains --root=... chain #alias post ...`).
2. **List Formats**:
   - `list order`: Each non-empty line contains exactly one 40-character action ID. Revoked actions are enclosed in tildes: `~<id>~`. Blank lines and surrounding whitespace are safely ignored.
   - `list begs` and `list revokes`: Each non-empty line contains exactly one 40-character action ID.
   - `list dag`: Returned as raw ASCII text directly from stdout.
3. **Chains Dir Format**:
   - `chains dir`: Each non-empty line contains one chain alias (e.g. `#chat`).
4. **Action ID Format**:
   - Action IDs are 40-character hexadecimal strings (SHA-1 hashes). This library validates and requires full IDs to prevent ambiguous prefix matching.
   - Commands that create or modify actions (`post`, `like`, `dislike`, `revoke`, `unrevoke`) print the resulting action ID on the last non-empty line of stdout. If stdout is empty, an error is returned.
5. **Chain ID Format**:
   - `chains add <alias> init|clone`: The resulting chain ID is printed on the last non-empty line of stdout and begins with `#`.
6. **Reputation Outputs**:
   - `reps action <id>`: Stdout contains a single integer representing the reputation score.
   - `reps author <pub>`: Stdout contains a single integer representing the reputation score.
   - `reps revoke <id>`: Stdout contains two space-separated integers `<author> <others>`.
   - `reps actions`: Output lines formatted as `<action_id> <reps>`.
   - `reps authors`: Output lines formatted as `<pub> <reps>`, where `<pub>` may contain spaces (e.g. OpenSSH keys `ssh-ed25519 <key>`).
   - `reps revokes`: Output lines formatted as `<action_id> <author_reps> <others_reps>`.
7. **Time Field**:
   - In `get metadata`, the `time` field represents the author's local clock and is inherently untrusted. The library exposes this value in `Metadata.Time` for inspection, but never uses it for consensus ordering or sorting.
8. **Error Strings**:
   - Freechains CLI prints errors to stderr. Substring matching is performed case-insensitively using `DefaultErrorRules` to classify errors into typed sentinels. Unmatched errors fall back to `*CommandError`.
