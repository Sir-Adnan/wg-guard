package install

import (
	"context"
	"encoding/json"
	"path"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

const PreparedBuildDir = "/var/cache/wg-guard/update-candidate"
const preparedBinary = PreparedBuildDir + "/binary"
const preparedReceipt = PreparedBuildDir + "/build.json"

// One prepared candidate is retained separately from the running service and
// independent manager. Preparing an older release cannot downgrade the manager.
func CacheMaintenanceBuild(ctx context.Context, h Host, b distribution.Build) error {
	if !h.IsRoot() {
		return terminalError("install.error.root")
	}
	if err := validateManagerBuild(b, false); err != nil {
		return err
	}
	digest, _, err := fileDigest(ctx, h, b.BinaryPath, 256<<20)
	if err != nil || digest != b.SHA256 {
		return terminalError("install.error.image.5")
	}
	if _, err := inspectContract(ctx, h, []string{b.BinaryPath}); err != nil {
		return err
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	if err := h.MkdirAll(PreparedBuildDir, 0o700); err != nil {
		return err
	}
	if err := validateManagerPath(ctx, h, PreparedBuildDir, true); err != nil {
		return err
	}
	if err := h.CopyFile(b.BinaryPath, preparedBinary, 0o755); err != nil {
		return err
	}
	stored, _, err := fileDigest(ctx, h, preparedBinary, 256<<20)
	if err != nil || stored != b.SHA256 {
		return terminalError("install.error.image.5")
	}
	b.BinaryPath = preparedBinary
	return writeJSON(h, preparedReceipt, b)
}

func LoadPreparedMaintenanceBuild(ctx context.Context, h Host) (distribution.Build, error) {
	for _, p := range []string{PreparedBuildDir, preparedBinary, preparedReceipt} {
		if err := validateManagerPath(ctx, h, p, p == PreparedBuildDir); err != nil {
			return distribution.Build{}, err
		}
	}
	raw, err := readRecord(h, preparedReceipt)
	if err != nil {
		return distribution.Build{}, err
	}
	var b distribution.Build
	if json.Unmarshal(raw, &b) != nil || b.BinaryPath != preparedBinary || path.Dir(b.BinaryPath) != PreparedBuildDir {
		return b, terminalError("install.error.state")
	}
	if err := validateManagerBuild(b, false); err != nil {
		return b, err
	}
	digest, _, err := fileDigest(ctx, h, b.BinaryPath, 256<<20)
	if err != nil || digest != b.SHA256 {
		return b, terminalError("install.error.image.5")
	}
	if _, err := inspectContract(ctx, h, []string{b.BinaryPath}); err != nil {
		return b, err
	}
	return b, nil
}
