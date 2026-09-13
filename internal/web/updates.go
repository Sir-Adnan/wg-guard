package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

type ReleaseCatalog interface {
	Releases(context.Context) ([]distribution.Release, error)
}

type updatesData struct {
	Available     bool
	ReleasesKnown bool
	Active        bool
	Error         string
	Releases      []distribution.Release
	Cores         []install.CoreBundle
	Status        updatequeue.Status
	PanelVersion  string
	ToolsVersion  string
}

func (s *Server) updatesData(r *http.Request) updatesData {
	d := s.updateRuntimeData()
	s.loadUpdateCatalog(r, &d)
	return d
}

func (s *Server) updateRuntimeData() updatesData {
	d := updatesData{PanelVersion: s.Version, ToolsVersion: s.ToolsVersion}
	if s.UpdateQueue != nil {
		d.Available = s.UpdateQueue.Available()
		if status, err := s.UpdateQueue.Status(); err == nil {
			d.Status = status
			d.Active = status.State == updatequeue.StateQueued || status.State == updatequeue.StateRunning
		}
	}
	d.Cores = install.ReviewedCoreBundles()
	return d
}

func (s *Server) loadUpdateCatalog(r *http.Request, d *updatesData) {
	if s.UpdateCatalog != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		if releases, err := s.UpdateCatalog.Releases(ctx); err == nil {
			d.Releases = releases
			d.ReleasesKnown = true
		}
	}
}

func (s *Server) handleUpdatesPage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "updates", "app", s.updatesData(r))
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	d := s.updateRuntimeData()
	priorID, priorState := r.URL.Query().Get("id"), r.URL.Query().Get("state")
	if priorID != "" && priorID == d.Status.ID && priorState == string(d.Status.State) {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if priorID != "" && priorID == d.Status.ID && !d.Active {
		// Refresh the complete catalog once when the host operation finishes so
		// disabled version actions and installed identities cannot stay stale.
		w.Header().Set("HX-Refresh", "true")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := s.partial(w, r, "updates", "update_status", d); err != nil {
		s.logError(r, "update status render", err)
	}
}

func (s *Server) handleUpdateRequest(w http.ResponseWriter, r *http.Request) {
	d := s.updateRuntimeData()
	if !d.Available || s.UpdateQueue == nil {
		d.Error = s.t(r, "updates.error.unavailable")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = s.render(w, r, "updates", "app", d)
		return
	}
	input := updatequeue.Input{
		Operation: updatequeue.Operation(strings.TrimSpace(r.PostFormValue("operation"))),
		Channel:   strings.TrimSpace(r.PostFormValue("channel")),
		Ref:       strings.TrimSpace(r.PostFormValue("ref")),
		Core:      strings.TrimSpace(r.PostFormValue("core")),
	}
	if input.Operation == updatequeue.OperationPanel || input.Operation == updatequeue.OperationAll {
		s.loadUpdateCatalog(r, &d)
	}
	if (input.Operation == updatequeue.OperationPanel || input.Operation == updatequeue.OperationAll) &&
		(input.Channel != "release" || !releaseListed(d.Releases, input.Ref)) {
		s.renderUpdateError(w, r, d, "updates.error.selection")
		return
	}
	if input.Operation == updatequeue.OperationCore || input.Operation == updatequeue.OperationAll {
		if _, err := install.SelectCore(input.Core); err != nil {
			s.renderUpdateError(w, r, d, "updates.error.selection")
			return
		}
	}
	status, err := s.UpdateQueue.Enqueue(r.Context(), input)
	if err != nil {
		if errors.Is(err, updatequeue.ErrBusy) {
			s.renderUpdateError(w, r, d, "updates.error.busy")
			return
		}
		if errors.Is(err, updatequeue.ErrInvalid) || errors.Is(err, updatequeue.ErrUnavailable) {
			s.renderUpdateError(w, r, d, "updates.error.selection")
			return
		}
		s.logError(r, "queue update request", err)
		s.renderUpdateError(w, r, d, "common.error_generic")
		return
	}
	s.audit(r, "update.requested", status.ID, map[string]any{
		"operation": input.Operation, "channel": input.Channel, "ref": input.Ref, "core": input.Core,
	})
	q := url.Values{"toast": {"updates.toast.queued"}}
	http.Redirect(w, r, "/updates?"+q.Encode(), http.StatusSeeOther)
}

func (s *Server) renderUpdateError(w http.ResponseWriter, r *http.Request, d updatesData, key string) {
	if !d.ReleasesKnown {
		s.loadUpdateCatalog(r, &d)
	}
	d.Error = s.t(r, key)
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = s.render(w, r, "updates", "app", d)
}

func releaseListed(releases []distribution.Release, ref string) bool {
	for _, release := range releases {
		if release.Tag == ref {
			return true
		}
	}
	return false
}
