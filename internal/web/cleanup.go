package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/cleanup"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
	"github.com/Sir-Adnan/wg-guard/internal/reseller"
)

type cleanupData struct {
	Form         operationalForm
	Owners       []reseller.Account
	Preview      *cleanup.Preview
	Storage      cleanup.Storage
	StorageKnown bool
	Error        string
}

func (s *Server) cleanupPageData(r *http.Request) cleanupData {
	d := cleanupData{Form: operationalForm{Values: map[string]string{"kind": "users", "status": "expired", "date_field": "expires_at", "after": "", "before": "", "owner": "", "include_queued": ""}}}
	if r.Method == http.MethodPost {
		d.Form = submittedOperationalForm(r, d.Form.Values)
	}
	if s.Resellers != nil {
		d.Owners, _ = s.Resellers.List(r.Context())
	}
	if info, err := cleanup.StorageInfo(r.Context(), s.DB); err == nil {
		d.Storage = info
		d.StorageKnown = true
	}
	return d
}

func (s *Server) handleCleanupPage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "cleanup", "app", s.cleanupPageData(r))
}

func cleanupFilter(r *http.Request) (cleanup.Filter, error) {
	f := cleanup.Filter{Kind: r.PostFormValue("kind"), Status: r.PostFormValue("status"), DateField: r.PostFormValue("date_field"), Owner: r.PostFormValue("owner")}
	f.IncludeQueued = r.PostFormValue("include_queued") == "1"
	for _, entry := range []struct {
		name  string
		value *string
	}{{"after", &f.After}, {"before", &f.Before}} {
		value := strings.TrimSpace(r.PostFormValue(entry.name))
		if value == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", value)
		if err != nil {
			return f, domain.E(domain.CodeInvalidRequest, "enter dates as YYYY-MM-DD")
		}
		*entry.value = t.UTC().Format(time.RFC3339)
	}
	return f, nil
}

func (s *Server) cleanupError(w http.ResponseWriter, r *http.Request, d cleanupData, err error) {
	var de *domain.Error
	if errors.As(err, &de) {
		key := "cleanup.invalid"
		if strings.Contains(de.Message, "selection changed") {
			key = "cleanup.stale"
		}
		if strings.Contains(de.Message, "preview") {
			key = "cleanup.preview_invalid"
		}
		d.Error = s.t(r, key)
	} else {
		s.logError(r, "cleanup", err)
		d.Error = s.newView(r).T("cleanup.failed")
	}
	_ = s.render(w, r, "cleanup", "app", d)
}

func (s *Server) handleCleanupPreview(w http.ResponseWriter, r *http.Request) {
	d := s.cleanupPageData(r)
	f, err := cleanupFilter(r)
	if err == nil {
		d.Preview, err = s.cleanup.Preview(r.Context(), f, adminFrom(r).ID)
	}
	if err != nil {
		s.cleanupError(w, r, d, err)
		return
	}
	_ = s.render(w, r, "cleanup", "app", d)
}

func (s *Server) handleCleanupExecute(w http.ResponseWriter, r *http.Request) {
	if !s.cleanupMu.TryLock() {
		s.redirectToast(w, r, "/cleanup", "cleanup.busy")
		return
	}
	defer s.cleanupMu.Unlock()
	result, err := s.cleanup.Execute(r.Context(), r.PostFormValue("preview_token"), adminFrom(r).ID)
	if err != nil {
		s.cleanupError(w, r, s.cleanupPageData(r), err)
		return
	}
	s.audit(r, "cleanup.executed", "", map[string]any{"kind": result.Filter.Kind, "rows": len(result.Rows), "devices": result.Devices, "owner": result.Filter.Owner})
	if result.Filter.Kind == "users" {
		s.runReconcile(r)
	}
	s.redirectToast(w, r, "/cleanup", "cleanup.done", strconv.Itoa(len(result.Rows)))
}

func (s *Server) handleCleanupOptimize(w http.ResponseWriter, r *http.Request) {
	compact := r.PostFormValue("compact") == "1"
	if compact && adminFrom(r).Role != auth.RoleOwner {
		s.surfaceError(w, r, http.StatusForbidden, "common.denied", "")
		return
	}
	if !s.cleanupMu.TryLock() {
		s.redirectToast(w, r, "/cleanup", "cleanup.busy")
		return
	}
	defer s.cleanupMu.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := cleanup.Optimize(ctx, s.DB, compact); err != nil {
		s.cleanupError(w, r, s.cleanupPageData(r), err)
		return
	}
	s.audit(r, "database.optimized", "", map[string]any{"compact": compact})
	s.redirectToast(w, r, "/cleanup", "cleanup.optimized")
}
