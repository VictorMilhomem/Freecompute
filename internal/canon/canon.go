package canon

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

var (
	// ErrUnsafeEntry is returned when a symlink, device, socket, or non-regular file is found.
	ErrUnsafeEntry = errors.New("unsafe file entry detected")

	// ErrTooLarge is returned when total bytes or file count exceeds configured limits.
	ErrTooLarge = errors.New("directory contents exceed limit")

	// ErrInvalidArgument is returned when input arguments are invalid.
	ErrInvalidArgument = errors.New("invalid argument")
)

// Limits bounds resource consumption during directory hashing.
type Limits struct {
	MaxBytes int64
	MaxFiles int
}

type fileEntry struct {
	relPath  string
	fullPath string
	size     int64
}

// DirectoryDigest computes a deterministic SHA-256 of the directory tree at root.
func DirectoryDigest(root string, lim Limits) ([32]byte, error) {
	if root == "" {
		return [32]byte{}, fmt.Errorf("%w: root directory cannot be empty", ErrInvalidArgument)
	}

	cleanRoot := filepath.Clean(root)
	rootInfo, err := os.Stat(cleanRoot)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to stat root directory: %w", err)
	}
	if !rootInfo.IsDir() {
		return [32]byte{}, fmt.Errorf("%w: root %q is not a directory", ErrInvalidArgument, root)
	}

	var entries []fileEntry
	var totalBytes int64

	err = filepath.Walk(cleanRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == cleanRoot {
			return nil
		}

		// Never follow symlinks; inspect the entry itself.
		linfo, err := os.Lstat(path)
		if err != nil {
			return err
		}

		if linfo.Mode()&os.ModeSymlink != 0 {
			rel, _ := filepath.Rel(cleanRoot, path)
			return fmt.Errorf("%w: symlink at %s", ErrUnsafeEntry, rel)
		}

		if linfo.IsDir() {
			return nil
		}

		if !linfo.Mode().IsRegular() {
			rel, _ := filepath.Rel(cleanRoot, path)
			return fmt.Errorf("%w: non-regular file at %s (mode %v)", ErrUnsafeEntry, rel, linfo.Mode())
		}

		rel, err := filepath.Rel(cleanRoot, path)
		if err != nil {
			return err
		}
		// Convert OS path separator to forward slash for reproducibility.
		slashRel := filepath.ToSlash(rel)

		entries = append(entries, fileEntry{
			relPath:  slashRel,
			fullPath: path,
			size:     linfo.Size(),
		})

		if lim.MaxFiles > 0 && len(entries) > lim.MaxFiles {
			return fmt.Errorf("%w: file count exceeded %d", ErrTooLarge, lim.MaxFiles)
		}

		totalBytes += linfo.Size()
		if lim.MaxBytes > 0 && totalBytes > lim.MaxBytes {
			return fmt.Errorf("%w: total bytes exceeded %d", ErrTooLarge, lim.MaxBytes)
		}

		return nil
	})
	if err != nil {
		return [32]byte{}, err
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].relPath < entries[j].relPath
	})

	hasher := sha256.New()
	buf := make([]byte, 32*1024)

	for _, entry := range entries {
		// Format: <relPath>\n<size>\n<content>\n
		if _, err := hasher.Write([]byte(entry.relPath + "\n")); err != nil {
			return [32]byte{}, err
		}
		sizeStr := strconv.FormatInt(entry.size, 10) + "\n"
		if _, err := hasher.Write([]byte(sizeStr)); err != nil {
			return [32]byte{}, err
		}

		f, err := os.Open(entry.fullPath)
		if err != nil {
			return [32]byte{}, fmt.Errorf("failed to open file %s: %w", entry.relPath, err)
		}
		copied, err := io.CopyBuffer(hasher, f, buf)
		_ = f.Close()
		if err != nil {
			return [32]byte{}, fmt.Errorf("failed to read file %s: %w", entry.relPath, err)
		}
		if copied != entry.size {
			return [32]byte{}, fmt.Errorf("file size changed while reading %s", entry.relPath)
		}

		if _, err := hasher.Write([]byte("\n")); err != nil {
			return [32]byte{}, err
		}
	}

	var out [32]byte
	copy(out[:], hasher.Sum(nil))
	return out, nil
}
