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
	Form          operationalForm
	Owners        []reseller.Account
	Preview       *cleanup.Preview
	Storage       cleanup.Storage
	StorageKnown  bool
	Error         string
	Selected      cleanup.Filter
	KindChoices   []string
	StatusChoices []string
	AllKinds      bool
	AllStatuses   bool
	AllOwners     bool
}

func (s *Server) cleanupPageData(r *http.Request) cleanupData {
	d := cleanupData{Form: operationalForm{Values: map[string]string{"date_field": "expires_at", "after": "", "before": "", "include_queued": ""}}, KindChoices: cleanup.KindChoices(), StatusChoices: cleanup.StatusChoices(), Selected: cleanup.Filter{Kinds: []string{"users"}, Statuses: []string{"expired"}, Owners: []string{"node"}}}
	if r.Method == http.MethodPost {
		d.Form = submittedOperationalForm(r, d.Form.Values)
		d.Selected = cleanupChoicesFromPost(r)
	}
	d.AllKinds = len(d.Selected.Kinds) == len(d.KindChoices)
	d.AllStatuses = len(d.Selected.Statuses) == len(d.StatusChoices)
	d.AllOwners = len(d.Selected.Owners) == 1 && d.Selected.Owners[0] == "*"
	if s.Resellers != nil {
		d.Owners, _ = s.Resellers.List(r.Context())
	}
	if info, err := cleanup.StorageInfo(r.Context(), s.DB); err == nil {
		d.Storage = info
		d.StorageKnown = true
	}
	return d
}

func cleanupChoicesFromPost(r *http.Request) cleanup.Filter {
	_ = r.ParseForm()
	f := cleanup.Filter{Kinds: append([]string(nil), r.PostForm["kinds"]...), Statuses: append([]string(nil), r.PostForm["statuses"]...), Owners: append([]string(nil), r.PostForm["owners"]...)}
	if r.PostFormValue("kinds_all") == "1" {
		f.Kinds = cleanup.KindChoices()
	}
	if r.PostFormValue("statuses_all") == "1" {
		f.Statuses = cleanup.StatusChoices()
	}
	if r.PostFormValue("owners_all") == "1" {
		f.Owners = []string{"*"}
	}
	return f
}

func (s *Server) handleCleanupPage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "cleanup", "app", s.cleanupPageData(r))
}

func cleanupFilter(r *http.Request) (cleanup.Filter, error) {
	f := cleanupChoicesFromPost(r)
	f.DateField = r.PostFormValue("date_field")
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
	s.audit(r, "cleanup.executed", "", map[string]any{"kinds": result.Filter.Kinds, "statuses": result.Filter.Statuses, "owners": result.Filter.Owners, "rows": len(result.Rows), "devices": result.Devices})
	if result.Users > 0 {
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
