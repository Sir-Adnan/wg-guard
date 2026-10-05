package backup

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

// PreparedInstall is an offline validated archive, not an installed-node handle.
// The archive is decrypted once into private staging; no password is retained.
// Target paths/TLS are supplied separately and archived boot config stays inactive.
type PreparedInstall struct {
	preview *PendingRestore
	report  *RestoreReport
	root    string
}

func PrepareInstall(ctx context.Context, archive, password string) (*PreparedInstall, error) {
	dir, err := os.MkdirTemp("", "wg-guard-install-preview-")
	if err != nil {
		return nil, verificationError(safetyError("data_inspection", err))
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Complete()
	s := &Service{Cfg: cfg}
	p, report, err := s.Stage(ctx, archive, password)
	if err != nil {
		return nil, err
	}
	if report.Inventory.EnabledOwners == 0 {
		return nil, safetyError("install_owner", nil)
	}
	keep = true
	return &PreparedInstall{preview: p, report: report, root: dir}, nil
}

func (p *PreparedInstall) RestoreReport() *RestoreReport { return p.report }
func (p *PreparedInstall) Close() error {
	if p == nil || p.root == "" {
		return nil
	}
	err := os.RemoveAll(p.root)
	if err == nil {
		p.root = ""
	}
	return err
}

// ApplyInitialData requires an empty target under exclusive DB/key ownership.
// It uses the existing paired replacement/recovery engine, never copies live WAL
// or activates archived TLS/host settings, and never initializes new credentials.
func (p *PreparedInstall) ApplyInitialData(ctx context.Context, cfg *config.Config, configPath string) error {
	if p == nil || p.root == "" || p.preview == nil {
		return safetyError("stage_incomplete", nil)
	}
	verified, err := loadStaged(p.preview.Dir)
	if err != nil {
		return err
	}
	s := &Service{Cfg: cfg, ConfigPath: configPath}
	lease, err := s.lockData(true)
	if err != nil {
		return err
	}
	defer lease.Close()
	for _, target := range s.restoreTargets() {
		if _, err := os.Lstat(target); !os.IsNotExist(err) {
			return safetyError("install_existing", err)
		}
	}
	for _, name := range []string{pendingDirName, transactionDir, RestoreGuardName} {
		if _, err := os.Lstat(filepath.Join(cfg.DataDir, name)); !os.IsNotExist(err) {
			return safetyError("install_existing", err)
		}
	}
	dir, err := os.MkdirTemp(cfg.DataDir, previewPrefix)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for name := range verified.Files {
		if err := ctx.Err(); err != nil {
			return verificationError(err)
		}
		if err := copyInitialMember(ctx, filepath.Join(verified.Dir, name), filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	copy := *verified
	copy.Dir = dir
	if err := s.writeStagedMeta(&copy, verified.Size); err != nil {
		return err
	}
	if _, err := s.approve(copy.PreviewID()); err != nil {
		return err
	}
	_, err = s.applyStaged(ctx)
	if err != nil {
		return errors.Join(err, s.recoverPair())
	}
	return nil
}

func copyInitialMember(ctx context.Context, src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	var buffer [64 << 10]byte
	defer clear(buffer[:])
	for {
		if err = ctx.Err(); err != nil {
			break
		}
		var count int
		count, err = in.Read(buffer[:])
		if count > 0 {
			if _, writeErr := out.Write(buffer[:count]); writeErr != nil {
				err = writeErr
				break
			}
		}
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			break
		}
	}
	if err == nil {
		err = out.Sync()
	}
	return verificationError(errors.Join(err, out.Close()))
}
