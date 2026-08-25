package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// UniquePath returns dir/stem+ext if that path doesn't already exist, or
// otherwise dir/"stem (2)"+ext, dir/"stem (3)"+ext, etc. — the first free
// name — so an OCR run never silently overwrites something already in the
// output directory. ext should include the leading dot (e.g. ".pdf").
func UniquePath(dir, stem, ext string) string {
	candidate := filepath.Join(dir, stem+ext)
	for i := 2; fileExists(candidate); i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
	}
	return candidate
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
