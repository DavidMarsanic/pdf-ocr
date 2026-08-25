package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/DavidMarsanic/pdf-ocr/internal/browser"
	"github.com/DavidMarsanic/pdf-ocr/internal/engine"
	"github.com/DavidMarsanic/pdf-ocr/internal/jobs"
	"github.com/DavidMarsanic/pdf-ocr/internal/paths"
)

// handleLanguages gives the frontend a single source of truth for which
// OCR languages are actually available on this machine, instead of
// hardcoding the list a second time in JS.
func (s *Server) handleLanguages(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"languages": s.Engine.Languages,
	})
}

// handleCreateJob accepts a multipart upload of one local PDF plus OCR
// options and runs ocrmypdf in the background, reporting progress over SSE.
func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid upload", "code": "bad-request"})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file uploaded", "code": "bad-request"})
		return
	}
	defer file.Close()

	origName := sanitizeFilename(header.Filename)
	if origName == "" {
		origName = "input.pdf"
	}
	if !strings.EqualFold(filepath.Ext(origName), ".pdf") {
		writeJSON(w, http.StatusBadRequest, map[string]string{
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

	job, ctx := s.Jobs.Create(s.ctx)
	scratch, err := paths.ScratchDir(job.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	srcPath := filepath.Join(scratch, origName)
	dst, err := os.Create(srcPath)
	if err != nil {
		os.RemoveAll(scratch)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		os.RemoveAll(scratch)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "saving upload: " + err.Error()})
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

	writeJSON(w, http.StatusOK, map[string]string{"jobId": job.ID})
}

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	if name == "." || name == ".." || name == "" {
		return ""
	}
	return name
}

func (s *Server) handleJobEvents(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, cancel := job.Subscribe()
	defer cancel()

	for {
		select {
		case e, open := <-ch:
			if !open {
				return
			}
			data, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			if e.Stage == "done" || e.Stage == "error" || e.Stage == "canceled" {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	job, ok := s.Jobs.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	job.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleReveal(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := browser.Reveal(req.Path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOpen(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := browser.Open(req.Path); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- helpers -----------------------------------------------------------

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body", "code": "bad-request"})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
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
