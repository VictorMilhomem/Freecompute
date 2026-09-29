// Package canon computes a deterministic SHA-256 digest of a directory tree
// for volunteer-computing task results.
//
// The digest is computed independently of host, locale, file permissions,
// owner, and modification timestamps. It is suitable for establishing consensus
// on task outputs across heterogeneous worker machines.
//
// Byte Layout Specification:
//
//  1. All regular files within root are discovered and their relative paths
//     computed with forward slash ("/") separators.
//  2. File entries are sorted in ascending lexicographic byte order by relative path.
//  3. For each file in sorted order, the following bytes are written to the SHA-256 hasher:
//     <relative_path> + "\n"
//     <file_size_in_decimal_ascii> + "\n"
//     <raw_file_bytes> + "\n"
//  4. If root contains no regular files, the digest is the SHA-256 of empty input:
//     e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855.
//
// Symlinks, devices, sockets, and non-regular files are rejected with ErrUnsafeEntry.
package canon
