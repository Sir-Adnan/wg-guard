package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/audit"
)

// Audit screen (audit.view): keyset-paged newest-first listing with action
// prefix and actor filters. Metadata JSON renders in an expandable row; it
// is written through the redaction allowlist upstream, so it is safe to show.
type auditData struct {
	Error string

	Records []audit.Record
	// NextCursor is the oldest ID on the page — the "older" link — set only
	// when the page was full (a cheap has-more heuristic).
	NextCursor int64
	Action     string
	ActorID    string
}

func (s *Server) handleAuditPage(w http.ResponseWriter, r *http.Request) {
	opts := audit.QueryOpts{
		Action:  strings.TrimSpace(r.URL.Query().Get("action")),
		ActorID: strings.TrimSpace(r.URL.Query().Get("actor")),
		Limit:   51, // one extra row detects has-more
	}
	if b := r.URL.Query().Get("before"); b != "" {
		if id, err := strconv.ParseInt(b, 10, 64); err == nil && id > 0 {
			opts.AfterID = id
		}
	}
	records, err := s.Audit.Query(r.Context(), opts)
	if err != nil {
		s.logError(r, "audit query", err)
	}
	d := auditData{Records: records, Action: opts.Action, ActorID: opts.ActorID}
	if len(records) > 50 {
		d.Records = records[:50]
		d.NextCursor = records[49].ID
	}
	_ = s.render(w, r, "audit", "app", d)
}
