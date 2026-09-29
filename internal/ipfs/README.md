# ipfs

`internal/ipfs` provides a typed client for content-addressed storage built on the **Kubo RPC API**.

## Deterministic Adds

To ensure that identical inputs yield identical CIDs regardless of node environment, `Store.Add` and `Store.AddDir` apply fixed parameters:
- **CID Version**: CIDv1
- **Multihash**: sha2-256 (default for CIDv1)
- **Leaves**: Raw leaves enabled (`raw-leaves=true`)
- **Chunker**: Default UnixFS 256 KiB size chunker (`size-262144`)
- **Pinning**: Pinned on add by default

## Resource Limits and Security

- **Bounded Reads**: `Store.Cat` wraps streams in a reader that terminates immediately with `ErrTooLarge` as soon as the specified byte limit is exceeded.
- **Safe Materialization (`Store.Fetch`)**:
  - Rejects symlinks, hardlinks, FIFOs, devices, and non-regular files with `ErrUnsafeContent`.
  - Rejects path traversal attempts (`..`, absolute paths, directory separators in filenames).
  - Enforces `MaxBytes`, `MaxFiles`, and `MaxDepth` while streaming to disk.
  - Refuses to write into non-empty destination directories (`ErrInvalidArgument`).
  - Cleans up all created files and directories on any error.
- **Local-Only RPC**: By default, connections are restricted to loopback addresses (`127.0.0.1`, `localhost`, `::1`) or UNIX sockets. Non-local addresses require `Config.AllowRemote = true`.
- **Garbage Collection**: Calling `Store.Unpin` removes pins but does not trigger Kubo garbage collection (`ipfs repo gc`). Unpinned blocks are reclaimed when the daemon runs GC.
