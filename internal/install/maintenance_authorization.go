package install

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/Sir-Adnan/wg-guard/internal/auth"
	"github.com/Sir-Adnan/wg-guard/internal/backup"
	"github.com/Sir-Adnan/wg-guard/internal/config"
	"github.com/Sir-Adnan/wg-guard/internal/database"
)

var ErrMaintenanceAuthorization = errors.New("maintenance authorization is no longer valid")

// AuthorizeMaintenance reads the existing account without migrations or loading
// keys. Shared ownership remains held until the host job ends, preventing a
// concurrent restore from replacing the authorization source underneath it.
func AuthorizeMaintenance(ctx context.Context, h Host, id string) (func(), error) {
	st, err := LoadState(h)
	if err != nil || st == nil {
		return nil, ErrMaintenanceAuthorization
	}
	cfg, err := ReadBootConfig(h, st.ConfigPath)
	if err != nil {
		return nil, ErrMaintenanceAuthorization
	}
	if filepath.Clean(cfg.DataDir) != filepath.Clean(DataDir) {
		return nil, ErrMaintenanceAuthorization
	}
	return authorizeMaintenanceData(ctx, cfg, id)
}

func authorizeMaintenanceData(ctx context.Context, cfg *config.Config, id string) (func(), error) {
	service := &backup.Service{Cfg: cfg}
	lease, err := service.OpenData(false)
	if err != nil {
		return nil, ErrMaintenanceAuthorization
	}
	db, err := database.Open(cfg.DatabasePath, database.Options{ReadOnly: true, MaxConns: 1})
	if err != nil {
		lease.Close()
		return nil, ErrMaintenanceAuthorization
	}
	defer db.Close()
	var role auth.Role
	var raw string
	var enabled bool
	err = db.QueryRowContext(ctx, `SELECT role,substr(permissions,1,8193),enabled FROM admins WHERE id=? AND reseller_id IS NULL`, id).Scan(&role, &raw, &enabled)
	var scopes []string
	if err != nil || len(raw) > 8192 || !role.Valid() || json.Unmarshal([]byte(raw), &scopes) != nil || !enabled || !auth.Authorized(role, scopes, auth.ScopeUpdateManage) {
		lease.Close()
		return nil, ErrMaintenanceAuthorization
	}
	return lease.Close, nil
}
