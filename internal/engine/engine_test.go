package engine

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDetectLanguages(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-tesseract")
	// A minimal stand-in for `tesseract --list-langs`: prints the same
	// header-then-codes shape tesseract actually uses, mixing real
	// language codes with pseudo-models (osd) that must be filtered out
	// because they're absent from knownLanguages.
	script := "#!/bin/sh\n" +
		"echo 'List of available languages in \"/fake\" (4):'\n" +
		"echo 'eng'\n" +
		"echo 'fra'\n" +
		"echo 'osd'\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	langs := detectLanguages(fake)
	if len(langs) != 2 {
		t.Fatalf("detectLanguages() = %+v, want exactly eng and fra", langs)
	}
	codes := map[string]bool{langs[0].Code: true, langs[1].Code: true}
	if !codes["eng"] || !codes["fra"] {
		t.Errorf("detectLanguages() codes = %+v, want eng and fra", codes)
	}
}

func TestDetectLanguages_ToolMissing(t *testing.T) {
	if got := detectLanguages(filepath.Join(t.TempDir(), "does-not-exist")); got != nil {
		t.Errorf("detectLanguages() with a missing binary = %+v, want nil", got)
	}
}

func TestOCRmyPDFVersion(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ocrmypdf")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho '17.10.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	v, err := ocrmypdfVersion(fake)
	if err != nil {
		t.Fatalf("ocrmypdfVersion() unexpected err: %v", err)
	}
	if v != "17.10.0" {
		t.Errorf("ocrmypdfVersion() = %q, want %q", v, "17.10.0")
	}
}

func TestCheckToolVersions(t *testing.T) {
	dir := t.TempDir()
	matching := filepath.Join(dir, "matching")
	if err := os.WriteFile(matching, []byte("#!/bin/sh\necho '"+expectedOCRmyPDFVersion+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if notes := checkToolVersions(matching); notes != nil {
		t.Errorf("checkToolVersions() with matching version = %v, want nil", notes)
	}

	drifted := filepath.Join(dir, "drifted")
	if err := os.WriteFile(drifted, []byte("#!/bin/sh\necho '99.0.0'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if notes := checkToolVersions(drifted); len(notes) != 1 {
		t.Errorf("checkToolVersions() with drifted version = %v, want exactly one note", notes)
	}
}

// runAndCapture runs a small generated shell script, capturing only its
// stderr — the same way Engine.OCR does — to produce a real *exec.ExitError
// with a known exit code, rather than hand-constructing one.
func runAndCapture(t *testing.T, script string, exitCode int) (error, string) {
	t.Helper()
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake")
	body := "#!/bin/sh\n" + script + "\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(fake)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	return err, stderr.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func TestClassifyOCRError(t *testing.T) {
	cases := []struct {
		name     string
		exitCode int
		wantErr  error
	}{
		{"already OCRed", 6, ErrAlreadyOCRed},
		{"encrypted", 8, ErrEncryptedPDF},
		{"missing language data", 3, ErrMissingLanguageData},
		{"bad input file", 2, ErrInputFile},
		{"unrecognized code falls back to generic", 15, ErrOCRFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err, stderr := runAndCapture(t, "echo 'some message' 1>&2", tc.exitCode)
			got := classifyOCRError(err, stderr)
			if !errors.Is(got, tc.wantErr) {
				t.Errorf("classifyOCRError() = %v, want wrapping %v", got, tc.wantErr)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate(""); got != "failed with no output" {
		t.Errorf("truncate(\"\") = %q", got)
	}
	short := "a short error"
	if got := truncate(short); got != short {
		t.Errorf("truncate(short) = %q, want unchanged", got)
	}
	long := make([]byte, maxErrLen+50)
	for i := range long {
		long[i] = 'x'
	}
	got := truncate(string(long))
	if len(got) != maxErrLen+len("…") {
		t.Errorf("truncate(long) length = %d, want %d", len(got), maxErrLen+len("…"))
	}
}
