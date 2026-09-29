# canon

`internal/canon` provides deterministic SHA-256 directory tree digest computation for volunteer-computing task results (`result_hash`).

## Deterministic Byte Layout

Entries are discovered recursively, mapped to relative paths with forward slash (`/`) separators, and sorted in lexicographic byte order. For each file, the following stream is hashed:

```
<rel_path>\n
<file_size_decimal>\n
<file_content>\n
```

If the directory contains no regular files, the digest is the SHA-256 of empty input (`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`).

## Security & Safety

- Rejects symlinks, hardlinks to outside the tree, device files, FIFOs, and sockets (`ErrUnsafeEntry`).
- Disallows unbounded traversal: enforces `Limits.MaxBytes` and `Limits.MaxFiles` (`ErrTooLarge`).
- Ignores file permissions (mode bits), owner (UID/GID), and modification times (mtime).
