// Package layout defines current node/host path ownership without performing
// filesystem operations or changing a deployment. Phase 17 owns any migration.
package layout

import (
	"path/filepath"

	"github.com/Sir-Adnan/wg-guard/internal/domain"
)

const (
	ConfigDir          = "/etc/wg-guard"
	DataDir            = "/var/lib/wg-guard"
	ConfigFile         = ConfigDir + "/wg-guard.toml"
	HostStateDir       = "/var/lib/wg-guard-host"
	DomainDir          = ConfigDir + "/domains"
	DomainPolicy       = DomainDir + "/active.json"
	DomainChallenges   = HostStateDir + "/domain-challenges"
	InstallState       = HostStateDir + "/install-state.json"
	LifecycleJournal   = HostStateDir + "/lifecycle.json"
	LifecycleArtifacts = HostStateDir + "/lifecycle"
	DeploymentDir      = "/opt/wg-guard"
	ComposeFile        = DeploymentDir + "/compose.yaml"
	HostBinary         = "/usr/local/bin/wg-guard"
	ManagerCache       = "/var/cache/wg-guard"
	BuildStaging       = ManagerCache + "/staging"
	DatabaseName       = "wg-guard.db"
	MasterKeyName      = "master.key"
)

func DatabaseFile(dataDir string) string  { return filepath.Join(dataDir, DatabaseName) }
func MasterKeyFile(dataDir string) string { return filepath.Join(dataDir, MasterKeyName) }

// CheckManagedData is a closed host restore boundary. Manual node sessions may
// use validated alternate layouts; managed replacement may not invent targets.
func CheckManagedData(configPath, dataDir, databasePath, keyPath string) error {
	if configPath != ConfigFile || dataDir != DataDir || databasePath != DatabaseFile(DataDir) || keyPath != MasterKeyFile(DataDir) {
		return domain.E(domain.CodeConfigInvalid, "managed restore requires the standard configuration/database/key layout")
	}
	return nil
}
