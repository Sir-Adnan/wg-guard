package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

const RestoreGuardName = "restore.lifecycle-blocked"

// CheckOpen blocks other processes from touching data during coordinated restore.
func (s *Service) CheckOpen() error {
	for _, name := range []string{RestoreGuardName, transactionDir} {
		if _, err := os.Lstat(filepath.Join(s.Cfg.DataDir, name)); !os.IsNotExist(err) {
			return safetyError("open_blocked", nil)
		}
	}
	return nil
}

// StageRecovery verifies the recorded archive identity around the streaming
// original-schema stage. Any drift discards the preview and blocks rollback.
func (s *Service) StageRecovery(ctx context.Context, path, password, digest string, encrypted bool) (*PendingRestore, *RestoreReport, error) {
	if !validHash(digest) || recoveryArchiveHash(ctx, path) != digest || fileEncrypted(path) != encrypted {
		return nil, nil, safetyError("archive_identity", nil)
	}
	p, r, err := s.StageOriginal(ctx, path, password)
	if err != nil {
		return nil, nil, err
	}
	if recoveryArchiveHash(ctx, path) != digest || r.Encrypted != encrypted {
		_ = s.DiscardPreview(p.PreviewID())
		return nil, nil, safetyError("archive_drift", nil)
	}
	return p, r, nil
}

func recoveryArchiveHash(ctx context.Context, path string) string {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 8<<30 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(restoreReader{ctx, f}, (8<<30)+1))
	if err != nil || n > 8<<30 {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
