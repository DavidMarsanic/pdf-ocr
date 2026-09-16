// Package server exposes the pdf-ocr engine over a small JSON+SSE HTTP API,
// bound to loopback only, for the embedded browser-based UI. Shared server
// plumbing (loopback bind, idle-timeout shutdown, job events/cancel/
// reveal/open routes) comes from brightencode-appkit; this package only
// adds the routes specific to OCR.
package server

import (
	"context"
	"net/http"

	appkit "github.com/DavidMarsanic/brightencode-appkit/server"
	"github.com/DavidMarsanic/pdf-ocr/internal/engine"
	"github.com/DavidMarsanic/pdf-ocr/web"
)

// maxUploadBytes caps one request's total multipart size. Scanned PDFs can
// get large — a few hundred image-heavy pages — so this is generous
// headroom, matching document-converter's cap for a similarly document-
// shaped upload.
const maxUploadBytes = 256 << 20 // 256MB

type Server struct {
	*appkit.Server
	Engine           *engine.Engine
	DefaultOutputDir string
}

func New(ctx context.Context, eng *engine.Engine, defaultOutputDir string) *Server {
	return &Server{
		Server:           appkit.New(ctx, 0),
		Engine:           eng,
		DefaultOutputDir: defaultOutputDir,
	}
}

func (s *Server) Start(port int) (string, error) {
	return s.Server.Start(port, web.Static, func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/languages", s.handleLanguages)
		mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	})
}
