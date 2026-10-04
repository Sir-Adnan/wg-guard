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
	dir, err := os.MkdirTemp("", "wg-guard-verify-")
	if err != nil {
		return nil, safetyError("data_inspection", err)
	}
	defer os.RemoveAll(dir)
	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.Complete()
	service := &Service{Cfg: cfg}
	_, report, err := service.Stage(ctx, path, password)
	return report, err
}
