package web

import (
	"io"
	"net/http"
	"net/url"

	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

// handleBackupImport accepts the panel's native multipart form without
// buffering the archive. The first field must be the small CSRF token; only
// after it validates do we stream the following file into the private sink.
func (s *Server) handleBackupImport(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		s.surfaceError(w, r, http.StatusBadRequest, "common.error_validation", "")
		return
	}
	csrfPart, err := mr.NextPart()
	if err != nil || csrfPart.FormName() != csrfField || csrfPart.FileName() != "" {
		s.surfaceError(w, r, http.StatusForbidden, "error.csrf", "")
		return
	}
	presented, readErr := io.ReadAll(io.LimitReader(csrfPart, maxStreamingCSRFBytes+1))
	closeErr := csrfPart.Close()
	tok, _ := r.Context().Value(ctxSession).(string)
	if readErr != nil || closeErr != nil || len(presented) > maxStreamingCSRFBytes ||
		!csrfValid(tok, string(presented)) {
		s.surfaceError(w, r, http.StatusForbidden, "error.csrf", "")
		return
	}

	archivePart, err := mr.NextPart()
	if err != nil || archivePart.FormName() != "archive" || archivePart.FileName() == "" {
		s.surfaceError(w, r, http.StatusBadRequest, "common.error_validation", "")
		return
	}
	info, importErr := s.Backup.Import(r.Context(), archivePart.FileName(), archivePart)
	closeErr = archivePart.Close()
	if importErr != nil {
		s.backupError(w, r, importErr)
		return
	}
	if closeErr != nil {
		_ = s.Backup.Delete(r.Context(), info.Name)
		s.backupError(w, r, closeErr)
		return
	}
	s.audit(r, "backup.imported", info.Name, map[string]any{"size": info.Size})
	q := url.Values{"tab": {"restore"}, "restore": {info.Name}, "toast": {"backups.toast.imported"}}
	http.Redirect(w, r, "/backups?"+q.Encode()+"#restore-workbench", http.StatusSeeOther)
}

// handleBackupRestore verifies + stages an archive and redirects to its durable review
// card. Nothing is applied until the operator confirms AND the service
// restarts (the swap happens before the database is opened).
func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	name := r.PostFormValue("name")
	password := r.PostFormValue("password")
	f, _, err := s.Backup.Open(name)
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	path := f.Name()
	f.Close()

	pr, _, err := s.Backup.StageReview(r.Context(), path, password)
	if err != nil {
		switch domain.CodeOf(err) {
		case domain.CodeInvalidRequest, domain.CodeNotFound:
			d := s.backupsData(r)
			d.Error = backup.ErrorText(err, s.localeFor(r))
			d.RestoreName = name
			_ = s.render(w, r, "backups", "app", d)
		default:
			s.backupError(w, r, err)
		}
		return
	}
	s.audit(r, "backup.restore_staged", pr.Archive, nil)
	q := url.Values{"tab": {"restore"}, "preview": {pr.PreviewID()}}
	http.Redirect(w, r, "/backups?"+q.Encode()+"#restore-review-title", http.StatusSeeOther)
}

// handleBackupVerify exercises the full archive/migration/secret gate without
// retaining decrypted preview data or offering a restore confirmation.
func (s *Server) handleBackupVerify(w http.ResponseWriter, r *http.Request) {
	name := r.PostFormValue("name")
	f, _, err := s.Backup.Open(name)
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	path := f.Name()
	f.Close()
	report, err := s.Backup.Verify(r.Context(), path, r.PostFormValue("password"))
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.verified", name, map[string]any{"sha256": report.ArchiveSHA256})
	d := s.backupsData(r)
	d.Verification, d.Review = true, report
	d.ReportCounts = backupReportCounts(report)
	d.Warnings = backup.WarningTexts(report.Warnings, s.localeFor(r))
	_ = s.render(w, r, "backups", "app", d)
}

// handleBackupRestoreConfirm acknowledges the review; the staged payload
// applies at the next restart.
func (s *Server) handleBackupRestoreConfirm(w http.ResponseWriter, r *http.Request) {
	pending, err := s.Backup.ApproveReview(r.Context(), r.PostFormValue("preview"))
	if err != nil || pending == nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.restore_confirmed", pending.Archive, nil)
	s.redirectToast(w, r, "/backups?tab=restore", "backups.toast.restore_confirmed")
}

// handleBackupRestoreCancel discards the staged restore.
func (s *Server) handleBackupRestoreCancel(w http.ResponseWriter, r *http.Request) {
	var err error
	if id := r.PostFormValue("preview"); id != "" {
		err = s.Backup.DiscardPreview(id)
	} else {
		err = s.Backup.CancelPending(r.PostFormValue("pending"))
	}
	if err != nil {
		s.backupError(w, r, err)
		return
	}
	s.audit(r, "backup.restore_cancelled", "", nil)
	s.redirectToast(w, r, "/backups?tab=restore", "backups.toast.restore_cancelled")
}
