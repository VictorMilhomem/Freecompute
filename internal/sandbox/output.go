package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
)

// VerifyOutputDir inspects the output directory after container completion.
// Symlinks, devices, and special files are strictly fatal because output
// contents will be hashed and published to IPFS by the host; allowing symlinks
// could enable arbitrary host file exfiltration.
func VerifyOutputDir(dir string, maxBytes int64, maxFiles int) (int64, int, error) {
	var totalBytes int64
	var fileCount int

	cleanDir := filepath.Clean(dir)
	err := filepath.Walk(cleanDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == cleanDir {
			return nil
		}

		// Never follow symlinks; inspect the entry itself.
		linfo, err := os.Lstat(path)
		if err != nil {
			return err
		}

		if linfo.Mode()&os.ModeSymlink != 0 {
			rel, _ := filepath.Rel(cleanDir, path)
			return fmt.Errorf("%w: symlink detected at %s", ErrUnsafeOutput, rel)
		}

		if linfo.IsDir() {
			return nil
		}

		if !linfo.Mode().IsRegular() {
			rel, _ := filepath.Rel(cleanDir, path)
			return fmt.Errorf("%w: non-regular file at %s (mode %v)", ErrUnsafeOutput, rel, linfo.Mode())
		}

		fileCount++
		if maxFiles > 0 && fileCount > maxFiles {
			return fmt.Errorf("%w: file count %d exceeds limit %d", ErrOutputTooLarge, fileCount, maxFiles)
		}

		totalBytes += linfo.Size()
		if maxBytes > 0 && totalBytes > maxBytes {
			return fmt.Errorf("%w: total size %d bytes exceeds limit %d", ErrOutputTooLarge, totalBytes, maxBytes)
		}

		return nil
	})
	if err != nil {
		return totalBytes, fileCount, err
	}

	return totalBytes, fileCount, nil
}
