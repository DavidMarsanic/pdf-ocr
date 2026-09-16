package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	appkit "github.com/DavidMarsanic/brightencode-appkit/server"
	"github.com/DavidMarsanic/brightencode-appkit/jobs"
	"github.com/DavidMarsanic/brightencode-appkit/paths"
	"github.com/DavidMarsanic/pdf-ocr/internal/engine"
)

// handleLanguages gives the frontend a single source of truth for which
// OCR languages are actually available on this machine, instead of
// hardcoding the list a second time in JS.
func (s *Server) handleLanguages(w http.ResponseWriter, r *http.Request) {
	appkit.WriteJSON(w, http.StatusOK, map[string]any{
		"languages": s.Engine.Languages,
	})
}

// handleCreateJob accepts a multipart upload of one local PDF plus OCR
// options and runs ocrmypdf in the background, reporting progress over SSE.
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		appkit.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload", "code": "bad-request"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		appkit.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "no file uploaded", "code": "bad-request"})
		return
	}
	defer file.Close()

	origName := sanitizeFilename(header.Filename)
	if origName == "" {
		origName = "input.pdf"
	}
	if !strings.EqualFold(filepath.Ext(origName), ".pdf") {
		appkit.WriteJSON(w, http.StatusBadRequest, map[string]string{
			"error": "only PDF files are supported",
			"code":  "unsupported-format",
		})
		return
	}

	opts := engine.Options{
		Language:        strings.TrimSpace(r.FormValue("language")),
		OCRExistingText: r.FormValue("ocrExistingText") == "true",
		Cleanup:         r.FormValue("cleanup") == "true",
	}

	job, ctx := s.Jobs.Create(s.Ctx)
	scratch, err := paths.ScratchDir("pdf-ocr", job.ID)
	if err != nil {
		appkit.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	srcPath := filepath.Join(scratch, origName)
	dst, err := os.Create(srcPath)
	if err != nil {
		os.RemoveAll(scratch)
		appkit.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.RemoveAll(scratch)
		appkit.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "saving upload: " + err.Error()})
		return
	}
	dst.Close()

	go func() {
		// Cleans up the uploaded input only; the OCRed output lives in the
		// real output dir (DefaultOutputDir) and is never removed.
		defer os.RemoveAll(scratch)

		onProgress := func(p engine.Progress) {
			// The engine's own "done" tick carries no Path/Filename (it
			// doesn't have them yet at that point in OCR) — forwarding it
			// as-is would let it win the race to be the terminal SSE event
			// ahead of the richer one published below, leaving the client
			// with a done event that has nowhere to point.
			if p.Stage == "done" {
				return
			}
			job.Publish(jobs.Event{Stage: p.Stage, Message: p.Message})
		}
		result, err := s.Engine.OCR(ctx, srcPath, opts, s.DefaultOutputDir, onProgress)
		if err != nil {
			if ctx.Err() != nil {
				job.Publish(jobs.Event{Stage: "canceled"})
				return
			}
			code, msg := classifyCode(err)
			job.Publish(jobs.Event{Stage: "error", Message: msg, Code: code})
			return
		}
		job.Publish(jobs.Event{Stage: "done", Path: result.Path, Filename: result.Filename})
	}()

	appkit.WriteJSON(w, http.StatusOK, map[string]string{"jobId": job.ID})
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	if name == "." || name == ".." || name == "" {
		return ""
	}
	return name
}

// classifyCode maps an engine error to the short machine-readable code
// published in SSE error events, plus the human-readable message to go
// with it.
func classifyCode(err error) (code, message string) {
	switch {
	case errors.Is(err, engine.ErrMissingDependency):
		return "missing-dependency", err.Error()
	case errors.Is(err, engine.ErrMissingLanguageData):
		return "missing-language", err.Error()
	case errors.Is(err, engine.ErrAlreadyOCRed):
		return "already-ocred", err.Error()
	case errors.Is(err, engine.ErrEncryptedPDF):
		return "encrypted", err.Error()
	case errors.Is(err, engine.ErrInputFile):
		return "invalid-input", err.Error()
	case errors.Is(err, engine.ErrOCRFailed):
		return "ocr-failed", err.Error()
	default:
		return "error", err.Error()
	}
}
