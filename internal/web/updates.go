package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/distribution"
	"github.com/Sir-Adnan/wg-guard/internal/install"
	"github.com/Sir-Adnan/wg-guard/internal/updatequeue"
)

type ReleaseCatalog interface {
	Releases(context.Context) ([]distribution.Release, error)
}

func (s *Server) handleUpdatesPage(w http.ResponseWriter, r *http.Request) {
	_ = s.render(w, r, "updates", "app", s.updatesData(r))
}

func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	d := s.updateRuntimeData()
	d.CanManage = maintenanceCan(r, auth.ScopeUpdateManage)
	priorID, priorState := r.URL.Query().Get("id"), r.URL.Query().Get("state")
	priorRevision := r.URL.Query().Get("revision")
	if priorID != "" && priorID == d.Status.ID && priorState == string(d.Status.State) && (priorRevision == "" && len(d.Status.Steps) == 0 || priorRevision == strconv.FormatUint(d.Status.Revision, 10)) {
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
		Target:    updatequeue.Operation(strings.TrimSpace(r.PostFormValue("target"))),
	}
	selection := input
	if input.Operation == "execute" {
		input.Operation, input.Target = input.Target, ""
		input.ExpectedCommit = strings.TrimSpace(r.PostFormValue("expected_commit"))
		input.ExpectedSHA256 = strings.TrimSpace(r.PostFormValue("expected_sha256"))
		selection = input
	}
	if input.Operation == updatequeue.OperationPreflight || input.Operation == updatequeue.OperationDownload {
		selection.Operation, selection.Target = input.Target, ""
	}
	if selection.Operation == updatequeue.OperationPanel {
		selection.Core = ""
		input.Core = ""
	}
	if selection.Operation == updatequeue.OperationPanel || selection.Operation == updatequeue.OperationAll {
		s.loadUpdateCatalog(r, &d)
	}
	listed := releaseListed(d.Releases, selection.Ref)
	if !listed && selection.Channel == "release" && selection.Ref != "latest" && len(selection.Ref) <= 128 {
		if source, ok := s.UpdateCatalog.(exactReleaseCatalog); ok {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			release, err := source.ReleaseByTag(ctx, selection.Ref)
			cancel()
			listed = err == nil && release.Tag == selection.Ref
		}
	}
	if (selection.Operation == updatequeue.OperationPanel || selection.Operation == updatequeue.OperationAll) &&
		(selection.Channel != "release" || !listed) {
		s.renderUpdateError(w, r, d, "updates.error.selection")
		return
	}
	if selection.Operation == updatequeue.OperationCore || selection.Operation == updatequeue.OperationAll {
		if _, err := install.SelectCore(input.Core); err != nil {
			s.renderUpdateError(w, r, d, "updates.error.selection")
			return
		}
	}
	actor, actorID := "", ""
	if admin := adminFrom(r); admin != nil {
		actor = admin.Username
		actorID = admin.ID
	}
	var status updatequeue.Status
	var err error
	if raw := strings.TrimSpace(r.PostFormValue("execute_at")); raw != "" && (input.Operation == updatequeue.OperationPanel || input.Operation == updatequeue.OperationCore || input.Operation == updatequeue.OperationAll) {
		at, parseErr := time.Parse("2006-01-02T15:04", raw)
		if parseErr != nil {
			s.renderUpdateError(w, r, d, "updates.error.schedule")
			return
		}
		status, err = s.UpdateQueue.ScheduleFor(r.Context(), input, actor, actorID, at)
	} else {
		status, err = s.UpdateQueue.EnqueueFor(r.Context(), input, actor, actorID)
	}
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
	q := url.Values{"toast": {"updates.toast.queued"}, "tab": {"operations"}}
	if input.Ref != "" {
		q.Set("version", input.Ref)
	}
	if input.Core != "" {
		q.Set("core", input.Core)
	}
	if selection.Operation == updatequeue.OperationCore {
		q.Set("component", "core")
	}
	q.Set("scope", string(selection.Operation))
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
