package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// lookPath resolves name via the standard PATH lookup, falling back to a
// short list of common install locations for the current OS if that fails.
//
// This exists because a GUI-launched process on macOS — whether opened via
// Finder/LaunchServices or spawned by securexe-launcher — does not inherit
// the user's interactive shell PATH. It gets the OS's minimal default
// (typically /usr/bin:/bin:/usr/sbin:/sbin), which excludes Homebrew's
// install directories entirely. A terminal-launched build never hits this,
// because the shell already sourced .zprofile/.zshrc and put Homebrew on
// PATH — which is exactly why this bug can pass local testing and then fail
// for a real double-clicked build. See also gif-maker/engine/engine.go and
// VideoClipDownloader/engine/tools.go, which have the same fallback for the
// same reason — keep the three in sync.
func lookPath(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return path, nil
	}
	candidateName := name
	if runtime.GOOS == "windows" && filepath.Ext(name) == "" {
		candidateName = name + ".exe"
	}
	for _, dir := range fallbackToolDirsFunc() {
		candidate := filepath.Join(dir, candidateName)
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// fallbackToolDirsFunc is a var (not a plain func call) so tests can
// substitute a temp directory instead of the real fallback locations.
var fallbackToolDirsFunc = fallbackToolDirs

// fallbackToolDirs lists common install locations for CLI tools that a
// GUI-launched process's minimal PATH won't include, ordered by likelihood.
func fallbackToolDirs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/opt/homebrew/bin", // Homebrew on Apple Silicon
			"/usr/local/bin",    // Homebrew on Intel Macs
			"/opt/local/bin",    // MacPorts
		}
	case "linux":
		return []string{
			"/usr/local/bin",
			"/snap/bin",
			"/var/lib/flatpak/exports/bin",
		}
	case "windows":
		dirs := []string{`C:\ProgramData\chocolatey\bin`}
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, "scoop", "shims"))
		}
		return dirs
	default:
		return nil
	}
}

// isExecutableFile reports whether path is a regular file that can be run
// as a command. Windows has no executable bit to check, so existence as a
// non-directory is treated as sufficient there.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0111 != 0
}
