// Package paths resolves where output files and scratch uploads live.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveDownloadsDir returns override (creating it if needed) or, if
// override is empty, the user's normal Downloads folder.
func ResolveDownloadsDir(override string) (string, error) {
	dir := override
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		dir = filepath.Join(home, "Downloads")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating output directory %s: %w", dir, err)
	}
	return dir, nil
}

// ScratchDir returns a fresh, empty directory under the OS temp dir for one
// job's uploaded input, never the user's real Downloads/home, and always
// cleaned up by the caller once the job ends.
func ScratchDir(jobID string) (string, error) {
	dir := filepath.Join(os.TempDir(), "pdf-ocr", jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating scratch directory: %w", err)
	}
	return dir, nil
}
