package backup

import (
	"context"
	"os"

	"github.com/Sir-Adnan/wg-guard/internal/config"
)

// VerifyArchive checks an archive in a private temporary installation. It opens
// no live node files, applies nothing, and needs neither Docker nor AWG tooling.
// Success covers archive integrity, supported migrations, references and every
// stored encrypted value; it is not a host/network or client-connectivity test.
func VerifyArchive(ctx context.Context, path, password string) (*RestoreReport, error) {
	return verifyArchiveIn(ctx, "", path, password)
}

// Verify uses the private data volume for temporary expansion: Docker's bounded
// /tmp is not a suitable destination for a portable database of up to 1 GiB.
// This leaves no preview/pending data and serializes crypto with archive creation.
func (s *Service) Verify(ctx context.Context, path, password string) (*RestoreReport, error) {
	release, err := s.claimInspection(ctx)
	if err != nil {
		return nil, verificationError(err)
	}
	defer release()
	return verifyArchiveIn(ctx, s.Cfg.DataDir, path, password)
}

func verifyArchiveIn(ctx context.Context, parent, path, password string) (*RestoreReport, error) {
	dir, err := os.MkdirTemp(parent, "wg-guard-verify-")
	if err != nil {
		return nil, safetyError("data_inspection", err)
	}
	defer os.RemoveAll(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Complete()
	service := &Service{Cfg: cfg}
	_, report, err := service.stage(ctx, path, password, false)
	return report, verificationError(err)
}
