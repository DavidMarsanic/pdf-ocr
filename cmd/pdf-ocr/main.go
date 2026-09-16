// Command pdf-ocr adds a searchable text layer to scanned PDFs via ocrmypdf
// (Tesseract under the hood), entirely on this machine. Bare invocation
// opens a local browser UI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/DavidMarsanic/brightencode-appkit/browser"
	"github.com/DavidMarsanic/brightencode-appkit/paths"
	"github.com/DavidMarsanic/pdf-ocr/internal/engine"
	"github.com/DavidMarsanic/pdf-ocr/internal/server"
)

const version = "0.1.0"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("pdf-ocr", flag.ContinueOnError)

	output := fs.String("output", "", "output directory (default: your Downloads folder)")
	port := fs.Int("port", 0, "local UI server port (default: automatic)")
	showVersion := fs.Bool("version", false, "print the version and exit")
	fs.Usage = func() { printUsage(fs) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}

	if *showVersion {
		fmt.Println("pdf-ocr " + version)
		return 0
	}

	widenPATH()

	outputDir, err := paths.ResolveDownloadsDir(*output)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	eng := engine.New()
	for _, note := range eng.VersionNotes {
		fmt.Fprintln(os.Stderr, "note:", note)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Startup never hard-fails even if ocrmypdf is missing — same non-fatal
	// philosophy as every other app in this family. The server still
	// starts and the window still opens; a missing ocrmypdf is instead
	// surfaced the first time someone actually tries to OCR something, via
	// the engine's CheckTools/OCR error path.
	srv := server.New(ctx, eng, outputDir)
	addr, err := srv.Start(*port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	fmt.Fprintln(os.Stderr, "PDF OCR running at", addr, "— press Ctrl+C to quit")

	// When a host process (securexe-launcher) is the one showing the UI —
	// in its own native window, so it can get a real Dock identity instead
	// of a spawned Chrome window — it sets SECUREXE_HOSTED before starting
	// us and watches this same stderr line to discover the URL.
	// OpenIfNotHosted no-ops in that case; opening our own Chrome window
	// too would just leave a second, redundant one.
	if err := browser.OpenIfNotHosted("pdf-ocr", addr+"/"); err != nil {
		fmt.Fprintln(os.Stderr, "couldn't open a window automatically:", err)
		fmt.Fprintln(os.Stderr, "open this URL manually:", addr+"/")
	}

	<-ctx.Done()
	return 0
}

// widenPATH adds common tool-install directories that a GUI-launched
// process often lacks. macOS gives an app spawned outside a shell (Finder,
// Spotlight, or another GUI app like a Securexe-style launcher — anything
// that isn't a terminal) a bare PATH of /usr/bin:/bin:/usr/sbin:/sbin,
// which doesn't include wherever Homebrew actually put ocrmypdf/tesseract.
// A plain terminal invocation already has all of this, so this only ever
// adds directories that exist on disk and aren't already present —
// nothing is removed or reordered.
func widenPATH() {
	home, _ := os.UserHomeDir()
	candidates := []string{
		"/opt/homebrew/bin", "/opt/homebrew/sbin", // Apple Silicon Homebrew
		"/usr/local/bin", "/usr/local/sbin", // Intel Homebrew / common Linux
		filepath.Join(home, ".local", "bin"),
	}

	current := os.Getenv("PATH")
	existing := map[string]bool{}
	for _, p := range filepath.SplitList(current) {
		existing[p] = true
	}

	var toAdd []string
	for _, dir := range candidates {
		if dir == "" || existing[dir] {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			toAdd = append(toAdd, dir)
		}
	}
	if len(toAdd) == 0 {
		return
	}
	toAdd = append(toAdd, current)
	os.Setenv("PATH", strings.Join(toAdd, string(os.PathListSeparator)))
}

func printUsage(fs *flag.FlagSet) {
	fmt.Fprint(os.Stderr, `pdf-ocr — add a searchable text layer to scanned PDFs, entirely on this
machine.

Bare invocation opens a local browser UI: drop a PDF, pick a language,
run OCR.

Usage:
  pdf-ocr          open the browser UI

Flags:
`)
	fs.PrintDefaults()
	fmt.Fprint(os.Stderr, `
Requires ocrmypdf on PATH (macOS: brew install ocrmypdf, which also
installs Tesseract, Ghostscript, and qpdf as its own dependencies). Not
bundled.
`)
}
