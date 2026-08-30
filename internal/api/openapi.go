package api

import (
	_ "embed"
	"net/http"
)

// openapiJSON is the hand-authored OpenAPI document (docs/architecture/api.md:
// "/openapi.json (+ lightweight /docs reference) is hand-authored and kept
// accurate by a route-coverage test"). It is a V1 compatibility contract:
// additive changes only.
//
//go:embed openapi.json
var openapiJSON []byte

// docsHTML is the lightweight API reference page: an index of the surface
// with a link to the full document. No JavaScript, no external assets.
//
//go:embed docs.html
var docsHTML []byte

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(openapiJSON)
}

func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(docsHTML)
}
