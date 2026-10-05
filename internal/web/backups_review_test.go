package web

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
)

func TestBackupVerificationHasNoConfirmationOrRetainedRestore(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	a, err := e.srv.Backup.Create(context.Background(), backup.CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	rec := e.postForm("/backups/verify", url.Values{"name": {a.Name}}, cookie)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "Archive verification passed") || strings.Contains(body, `action="/backups/restore/confirm"`) || !strings.Contains(body, "Archive SHA-256") {
		t.Fatal("verification confused with restore approval")
	}
	if p, err := e.srv.Backup.Pending(); err != nil || p != nil {
		t.Fatal("verification queued restore", err)
	}
	if previews, err := e.srv.Backup.Previews(); err != nil || len(previews) != 0 {
		t.Fatal("verification retained secret payload", err)
	}
}

func TestSavedBackupReviewRefreshDoesNotCreateAnotherPreview(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	a, err := e.srv.Backup.Create(context.Background(), backup.CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	rec := e.postForm("/backups/restore", url.Values{"name": {a.Name}}, cookie)
	if rec.Code != http.StatusSeeOther {
		t.Fatal("staging did not redirect")
	}
	location := rec.Header().Get("Location")
	for range 3 {
		page := e.get(location, cookie)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `action="/backups/restore/confirm"`) {
			t.Fatal("saved review could not be reopened")
		}
	}
	previews, err := e.srv.Backup.Previews()
	if err != nil || len(previews) != 1 {
		t.Fatal("refresh repeated staging", err)
	}
	if p, _ := e.srv.Backup.Pending(); p != nil {
		t.Fatal("review refresh approved restore")
	}
	// A page from another operator must not cancel a later pending request.
	first, err := e.srv.Backup.Approve(previews[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.srv.Backup.CancelPending(first.Identity); err != nil {
		t.Fatal(err)
	}
	second, _, err := e.srv.Backup.StageReview(context.Background(), a.Path, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.srv.Backup.Approve(second.PreviewID()); err != nil {
		t.Fatal(err)
	}
	rec = e.postForm("/backups/restore/cancel", url.Values{"pending": {first.Identity}}, cookie)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "no longer matches") {
		t.Fatal("stale request has no recovery guidance")
	}
	current, err := e.srv.Backup.PendingSummary()
	if err != nil || current == nil || current.Identity != second.Identity {
		t.Fatal("stale request cancelled the newer restore", err)
	}
}

func TestBackupWorkbenchUsesServerNavigationAndExistingPermissionBoundary(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	for _, item := range []struct{ path, present, absent string }{
		{"/backups", `id="archives-title"`, `id="restore-archive"`},
		{"/backups?tab=restore", `id="restore-archive"`, `id="sch-name"`},
		{"/backups?tab=schedules&schedule=new", `id="sch-name"`, `id="restore-archive"`},
		{"/backups?tab=delivery", `action="/backups/telegram-test"`, `id="restore-archive"`},
	} {
		body := e.get(item.path, cookie).Body.String()
		if !strings.Contains(body, item.present) || strings.Contains(body, item.absent) || strings.Contains(body, "backups.tab.") {
			t.Fatalf("section not isolated: %s", item.path)
		}
	}
	if _, err := e.admins.Create(context.Background(), "helper", testPassword, auth.RoleAdmin, []string{auth.ScopeUsersRead}); err != nil {
		t.Fatal(err)
	}
	helper := e.loginEN("helper")
	if rec := e.postForm("/backups/verify", url.Values{}, helper); rec.Code != http.StatusSeeOther {
		t.Fatal("verification bypassed backup.manage")
	}
	if rec := e.post("/backups/verify", url.Values{}, cookie, "bad-csrf"); rec.Code != http.StatusForbidden {
		t.Fatal("verification bypassed CSRF")
	}
	e.srv.Backup = nil
	if rec := e.postForm("/backups/verify", url.Values{}, cookie); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "Backup service is unavailable") {
		t.Fatal("stale form with an unavailable engine did not fail safely")
	}
}

func TestBackupArchiveSelectionCanAddressOlderCursorPages(t *testing.T) {
	e := newEnv(t)
	e.seedOwner()
	cookie := e.loginEN("owner")
	a, err := e.srv.Backup.Create(context.Background(), backup.CreateOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		name := "wg-guard-" + strings.Repeat("x", i+1) + ".wgg"
		if err := os.WriteFile(filepath.Join(filepath.Dir(a.Path), name), []byte("synthetic-envelope"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	data := e.srv.backupsData(httptestBackupRequest("/backups?tab=restore&restore=" + url.QueryEscape(a.Name)))
	if len(data.Archives) != 25 || data.NextCursor == "" {
		t.Fatal("archive rows are unbounded")
	}
	found := false
	for _, choice := range data.ArchiveChoices {
		found = found || choice.Name == a.Name
	}
	if !found || len(data.ArchiveChoices) > 26 {
		t.Fatal("older selected archive was lost or selection unbounded")
	}
	body := e.get("/backups?restore="+url.QueryEscape(a.Name), cookie).Body.String()
	if !strings.Contains(body, `value="`+a.Name+`" selected`) {
		t.Fatal("older selected archive is not bound to the form")
	}
}

func httptestBackupRequest(path string) *http.Request {
	r, _ := http.NewRequest(http.MethodGet, path, nil)
	return r
}
