package web

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
)

func maintenanceCan(r *http.Request, scope string) bool {
	a := adminFrom(r)
	return a != nil && auth.Authorized(a.Role, a.Permissions, scope)
}
func (s *Server) requireMaintenanceRead(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if maintenanceCan(r, auth.ScopeUpdateRead) {
			next(w, r)
			return
		}
		s.requirePermission(auth.ScopeUpdateManage, next)(w, r)
	}
}

func (s *Server) handleUpdateCancel(w http.ResponseWriter, r *http.Request) {
	if s.UpdateQueue == nil || s.UpdateQueue.Cancel(r.PostFormValue("id")) != nil {
		s.renderUpdateError(w, r, s.updateRuntimeData(), "updates.error.cancel")
		return
	}
	s.audit(r, "update.canceled", r.PostFormValue("id"), nil)
	http.Redirect(w, r, "/updates?tab=operations", http.StatusSeeOther)
}

func (s *Server) handleUpdateSnooze(w http.ResponseWriter, r *http.Request) {
	latest := s.updateCache.latestTag()
	if latest != "" {
		http.SetCookie(w, &http.Cookie{Name: "wg_update_snooze", Value: latest, Path: "/", MaxAge: 7 * 24 * 60 * 60, HttpOnly: true, Secure: s.cookieSecure(), SameSite: http.SameSiteLaxMode})
	}
	http.Redirect(w, r, "/updates", http.StatusSeeOther)
}

func (s *Server) handleUpdateReport(w http.ResponseWriter, r *http.Request) {
	if s.UpdateQueue == nil {
		http.NotFound(w, r)
		return
	}
	history, err := s.UpdateQueue.History()
	if err != nil {
		http.Error(w, s.t(r, "common.error_generic"), http.StatusServiceUnavailable)
		return
	}
	current, _ := s.UpdateQueue.Status()
	history = append(history, current)
	for _, operation := range history {
		if operation.ID != r.PathValue("id") {
			continue
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", `attachment; filename="wg-guard-operation.txt"`)
		fmt.Fprintf(w, "WG-Guard maintenance\nID: %s\nOperation: %s\nState: %s\nVersion: %s\nCore: %s\nFailure: %s\n", operation.ID, operation.Operation, operation.State, operation.Ref, operation.Core, operation.Failure)
		for _, step := range operation.Steps {
			fmt.Fprintf(w, "%s / %s: %s\n", step.Component, step.Stage, step.State)
		}
		return
	}
	http.NotFound(w, r)
}

func (s *Server) handleUpdateBackup(w http.ResponseWriter, r *http.Request) {
	if !maintenanceCan(r, auth.ScopeUpdateRead) && !maintenanceCan(r, auth.ScopeUpdateManage) || s.UpdateQueue == nil {
		http.NotFound(w, r)
		return
	}
	i, err := s.UpdateQueue.Inventory()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	b := i.Backup
	if id := r.URL.Query().Get("operation"); id != "" {
		b = nil
		history, err := s.UpdateQueue.History()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		for _, entry := range history {
			if entry.ID == id && entry.Outcome != nil {
				b = entry.Outcome.Backup
				break
			}
		}
		if b == nil {
			http.NotFound(w, r)
			return
		}
	}
	if b == nil {
		http.NotFound(w, r)
		return
	}
	dir := filepath.Join(s.UpdateQueue.Dir, "backups", "lifecycle-"+b.OperationID)
	for _, p := range []string{dir, filepath.Join(dir, b.Name)} {
		st, e := os.Lstat(p)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			http.NotFound(w, r)
			return
		}
	}
	f, err := os.Open(filepath.Join(dir, b.Name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil || hex.EncodeToString(hash.Sum(nil)) != b.SHA256 {
		http.Error(w, s.t(r, "updates.backup_changed"), http.StatusConflict)
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(b.Name, `"`, "")+`"; filename*=UTF-8''`+url.PathEscape(b.Name))
	http.ServeContent(w, r, b.Name, st.ModTime(), f)
}
