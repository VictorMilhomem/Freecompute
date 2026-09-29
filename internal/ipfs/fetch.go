package ipfs

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func safeMaterialize(ctx context.Context, node ContentNode, dest string, lim FetchLimits) (FetchResult, error) {
	if dest == "" {
		return FetchResult{}, fmt.Errorf("%w: destination path cannot be empty", ErrInvalidArgument)
	}

	cleanDest := filepath.Clean(dest)
	destExisted := false
	destStat, err := os.Stat(cleanDest)
	if err == nil {
		destExisted = true
		if !destStat.IsDir() {
			return FetchResult{}, fmt.Errorf("%w: destination %q is an existing file", ErrInvalidArgument, cleanDest)
		}
		entries, err := os.ReadDir(cleanDest)
		if err != nil {
			return FetchResult{}, fmt.Errorf("failed to read destination dir: %w", err)
		}
		if len(entries) > 0 {
			return FetchResult{}, fmt.Errorf("%w: destination %q is not empty", ErrInvalidArgument, cleanDest)
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(cleanDest, 0755); err != nil {
			return FetchResult{}, fmt.Errorf("failed to create destination dir: %w", err)
		}
	} else {
		return FetchResult{}, fmt.Errorf("failed to stat destination dir: %w", err)
	}

	createdPaths := make([]string, 0)
	cleanup := func() {
		if !destExisted {
			_ = os.RemoveAll(cleanDest)
			return
		}
		for i := len(createdPaths) - 1; i >= 0; i-- {
			_ = os.RemoveAll(createdPaths[i])
		}
	}

	var totalBytes int64
	var totalFiles int

	var writeNode func(currNode ContentNode, currDir string, depth int) error
	writeNode = func(currNode ContentNode, currDir string, depth int) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if lim.MaxDepth > 0 && depth > lim.MaxDepth {
			return fmt.Errorf("%w: directory depth exceeded %d", ErrTooLarge, lim.MaxDepth)
		}

		nodeType := currNode.Type()
		if nodeType == NodeSymlink || nodeType == NodeOther {
			return fmt.Errorf("%w: unsafe node type %v for %q", ErrUnsafeContent, nodeType, currNode.Name())
		}

		name := currNode.Name()
		if name != "" {
			if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
				return fmt.Errorf("%w: invalid filename %q", ErrUnsafeContent, name)
			}
		}

		if nodeType == NodeDir {
			targetDir := currDir
			if name != "" {
				targetDir = filepath.Join(currDir, name)
				rel, err := filepath.Rel(cleanDest, targetDir)
				if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
					return fmt.Errorf("%w: path %q escapes destination", ErrUnsafeContent, name)
				}
				if err := os.Mkdir(targetDir, 0755); err != nil && !os.IsExist(err) {
					return fmt.Errorf("failed to create directory: %w", err)
				}
				createdPaths = append(createdPaths, targetDir)
			}

			entries, err := currNode.Entries(ctx)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				if err := writeNode(entry, targetDir, depth+1); err != nil {
					return err
				}
			}
			return nil
		}

		// NodeFile
		targetFile := currDir
		if name != "" {
			targetFile = filepath.Join(currDir, name)
		} else {
			targetFile = filepath.Join(currDir, "content")
		}

		rel, err := filepath.Rel(cleanDest, targetFile)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("%w: path %q escapes destination", ErrUnsafeContent, name)
		}

		totalFiles++
		if lim.MaxFiles > 0 && totalFiles > lim.MaxFiles {
			return fmt.Errorf("%w: file count exceeded %d", ErrTooLarge, lim.MaxFiles)
		}

		r, err := currNode.Open()
		if err != nil {
			return err
		}
		defer r.Close()

		f, err := os.OpenFile(targetFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", targetFile, err)
		}
		createdPaths = append(createdPaths, targetFile)

		buf := make([]byte, 32*1024)
		for {
			select {
			case <-ctx.Done():
				_ = f.Close()
				return ctx.Err()
			default:
			}

			n, readErr := r.Read(buf)
			if n > 0 {
				totalBytes += int64(n)
				if lim.MaxBytes > 0 && totalBytes > lim.MaxBytes {
					_ = f.Close()
					return fmt.Errorf("%w: byte limit exceeded %d", ErrTooLarge, lim.MaxBytes)
				}
				if _, writeErr := f.Write(buf[:n]); writeErr != nil {
					_ = f.Close()
					return fmt.Errorf("failed to write to file: %w", writeErr)
				}
			}
			if readErr != nil {
				if readErr == io.EOF {
					break
				}
				_ = f.Close()
				return readErr
			}
		}

		if err := f.Close(); err != nil {
			return fmt.Errorf("failed to close file: %w", err)
		}

		return nil
	}

	// If root node has an empty name, start directly in cleanDest.
	err = writeNode(node, cleanDest, 0)
	if err != nil {
		cleanup()
		return FetchResult{}, err
	}

	return FetchResult{
		Bytes: totalBytes,
		Files: totalFiles,
	}, nil
}
