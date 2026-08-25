// Package engine wraps ocrmypdf — which itself wraps Tesseract, Ghostscript,
// and qpdf — behind a small, UI-agnostic interface: OCR. None of that
// toolchain is bundled; ocrmypdf is expected on PATH, and installing it via
// Homebrew pulls in the rest as its own dependencies automatically.
package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/DavidMarsanic/pdf-ocr/internal/paths"
)

// Engine is the whole pdf-ocr backend, independent of any UI.
type Engine struct {
	OCRmyPDFPath string

	// Languages is the set of OCR languages available on this machine —
	// knownLanguages intersected with what tesseract reports installed.
	// Always has at least English, even if tesseract couldn't be probed.
	Languages []Language

	// VersionNotes is populated by New — one entry if the resolved
	// ocrmypdf's version doesn't match what this engine was last tested
	// against. Non-blocking: a newer or older tool very often still works
	// fine — this just makes drift visible instead of silently invisible,
	// since nothing here pins or auto-updates it.
	VersionNotes []string

	toolsErr error // set by New only if ocrmypdf itself is missing; checked lazily
}

// New resolves ocrmypdf on PATH and always returns a usable Engine — it
// deliberately never fails outright. A double-clicked GUI app has no
// terminal to print a startup error to, so a hard failure here would mean
// the UI just never appears with no visible explanation. A missing tool is
// instead surfaced the first time OCR actually needs it, by which point
// there's a window that can show it.
func New() *Engine {
	e := &Engine{Languages: []Language{{"eng", "English"}}}

	ocrmypdf, err := lookPath("ocrmypdf")
	if err != nil {
		e.toolsErr = fmt.Errorf("%w: ocrmypdf — install it (macOS: brew install ocrmypdf; "+
			"this also pulls in Tesseract, Ghostscript, and everything else it needs); "+
			"https://ocrmypdf.readthedocs.io/en/latest/installation.html", ErrMissingDependency)
		return e
	}
	e.OCRmyPDFPath = ocrmypdf
	e.VersionNotes = checkToolVersions(ocrmypdf)

	// tesseract is only probed here to list which language packs are
	// actually installed — ocrmypdf calls it internally regardless of
	// whether this lookup succeeds, so a miss here is never fatal.
	if tesseractPath, err := lookPath("tesseract"); err == nil {
		if langs := detectLanguages(tesseractPath); len(langs) > 0 {
			e.Languages = langs
		}
	}

	return e
}

// expectedOCRmyPDFVersion is what this engine was actually last tested
// against — keep in sync with brightencode.json's matching dependency entry
// by hand; nothing enforces the two staying equal, but a mismatch between
// them here is a real bug.
const expectedOCRmyPDFVersion = "17.10.0"

// checkToolVersions runs `--version` against ocrmypdf and returns a
// human-readable note if its reported version doesn't match
// expectedOCRmyPDFVersion. Deliberately non-blocking — see VersionNotes'
// doc comment above.
func checkToolVersions(ocrmypdfPath string) []string {
	v, err := ocrmypdfVersion(ocrmypdfPath)
	if err != nil || v == expectedOCRmyPDFVersion {
		return nil
	}
	return []string{fmt.Sprintf(
		"ocrmypdf is %s, this app was last tested against %s — should still work, but if something breaks, that's the first thing to check",
		v, expectedOCRmyPDFVersion)}
}

// ocrmypdf's "--version" output is just "17.10.0\n" — no leading command
// name like pandoc's — but unlike pandoc/ffmpeg it's written to stderr, not
// stdout, so both streams are captured here rather than just stdout.
func ocrmypdfVersion(path string) (string, error) {
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// detectLanguages intersects knownLanguages with whatever tesseract
// reports installed via --list-langs, so the UI only ever offers languages
// that will actually work. Pseudo-models tesseract lists alongside real
// languages (orientation/script detection, digit-only, etc.) are filtered
// out for free — they simply aren't in knownLanguages.
func detectLanguages(tesseractPath string) []Language {
	out, err := exec.Command(tesseractPath, "--list-langs").Output()
	if err != nil {
		return nil
	}
	installed := map[string]bool{}
	for i, line := range strings.Split(string(out), "\n") {
		if i == 0 {
			continue // header line: `List of available languages in "..." (N):`
		}
		code := strings.TrimSpace(line)
		if code != "" {
			installed[code] = true
		}
	}

	var langs []Language
	for _, l := range knownLanguages {
		if installed[l.Code] {
			langs = append(langs, l)
		}
	}
	return langs
}

// CheckTools reports the missing-dependency error recorded by New, if any.
func (e *Engine) CheckTools() error {
	return e.toolsErr
}

// OCR runs ocrmypdf on inputPath, writing a searchable copy into outputDir
// under a name derived from inputPath's basename. onProgress is called once
// with "ocr" before ocrmypdf starts and once with "done" after it finishes
// successfully: ocrmypdf gives no reliable machine-readable incremental
// progress for a typical document, so this is a start/done tool, not a
// percent-streaming one.
func (e *Engine) OCR(ctx context.Context, inputPath string, opts Options, outputDir string, onProgress func(Progress)) (Result, error) {
	if e.toolsErr != nil {
		return Result{}, e.toolsErr
	}

	lang := opts.Language
	if lang == "" {
		lang = "eng"
	}

	stem := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	outputPath := paths.UniquePath(outputDir, stem+" (OCR)", ".pdf")

	if onProgress != nil {
		onProgress(Progress{Stage: "ocr"})
	}

	args := []string{"-l", lang}
	if opts.OCRExistingText {
		args = append(args, "--force-ocr")
	} else {
		args = append(args, "--skip-text")
	}
	if opts.Cleanup {
		args = append(args, "--deskew", "--rotate-pages")
	}
	args = append(args, inputPath, outputPath)

	cmd := exec.CommandContext(ctx, e.OCRmyPDFPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		return Result{}, classifyOCRError(err, stderr.String())
	}

	if onProgress != nil {
		onProgress(Progress{Stage: "done"})
	}

	return Result{Path: outputPath, Filename: filepath.Base(outputPath)}, nil
}

// ocrmypdf exit codes relevant here (see its own ocrmypdf/exceptions.py):
// 2 input_file, 3 missing_dependency (also covers a missing tesseract
// language pack), 6 already_done_ocr, 8 encrypted_pdf. Every other code
// falls through to the generic ErrOCRFailed with ocrmypdf's own stderr
// tail attached.
func classifyOCRError(err error, stderr string) error {
	msg := truncate(strings.TrimSpace(stderr))

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 6:
			return fmt.Errorf(`%w: turn on "Also OCR pages that already have text" to redo it`, ErrAlreadyOCRed)
		case 8:
			return fmt.Errorf("%w: remove the password first (Securexe's PDF Toolkit can do this), then try again", ErrEncryptedPDF)
		case 3:
			return fmt.Errorf("%w: %s", ErrMissingLanguageData, msg)
		case 2:
			return fmt.Errorf("%w: %s", ErrInputFile, msg)
		}
	}
	return fmt.Errorf("%w: %s", ErrOCRFailed, msg)
}

// maxErrLen caps an ocrmypdf stderr dump to a reasonable length, so a huge
// Ghostscript error doesn't blow up the UI.
const maxErrLen = 500

func truncate(s string) string {
	if s == "" {
		return "failed with no output"
	}
	if len(s) <= maxErrLen {
		return s
	}
	return s[:maxErrLen] + "…"
}
