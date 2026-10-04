package backup

import (
	"context"
	"os"
	"path/filepath"
)

const preMigrationKeep = 5

// MigrateNode is the automatic live-node migration gate shared by startup and
// host data commands. Existing data is archived under exclusive lifetime ownership
// before any migration. Fresh databases need no archive; inspection or backup
// failure blocks all schema writes. Staged restore copies use their own migrator.
func (s *Service) MigrateNode(ctx context.Context, lease *DataLease) error {
	if s.DB == nil || s.Cfg == nil {
		return safetyError("migration_inspection", nil)
	}
	status, err := s.DB.MigrationStatus(ctx)
	if err != nil {
		return safetyError("migration_inspection", err)
	}
	if status.Pending == 0 {
		return nil
	}
	if lease == nil || lease.file == nil {
		return safetyError("data_busy", nil)
	}
	ownedLock, err := lease.file.Stat()
	if err != nil {
		return safetyError("data_busy", err)
	}
	nodeLock, err := os.Lstat(filepath.Join(s.Cfg.DataDir, dataLeaseName))
	if err != nil || !nodeLock.Mode().IsRegular() || !os.SameFile(ownedLock, nodeLock) {
		return safetyError("data_busy", err)
	}
	if err := lease.RequireExclusive(); err != nil {
		return err
	}
	// Recheck once admission is exclusive; no live contender can migrate the
	// pair or swap its key while the snapshot/archive is prepared.
	status, err = s.DB.MigrationStatus(ctx)
	if err != nil {
		return safetyError("migration_inspection", err)
	}
	if status.Applied > 0 && status.Pending > 0 {
		dir := filepath.Join(s.Cfg.DataDir, "backups-auto")
		result, err := s.createArchive(ctx, CreateOpts{Reason: "pre-migration", Dir: dir}, "", lease)
		if err != nil {
			return safetyError("migration_backup", err)
		}
		if s.Log != nil {
			s.Log.Info("pre-migration backup verified", "archive", result.Name)
		}
		// Publish the new recovery archive before considering old retention.
		// A retention failure cannot invalidate the already verified archive.
		if _, err := s.pruneDir(dir, preMigrationKeep); err != nil && s.Log != nil {
			s.Log.Warn("pre-migration retention failed")
		}
	}
	return s.DB.Migrate(ctx, s.Log)
}
