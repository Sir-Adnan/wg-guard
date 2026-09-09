package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

var managerRef = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9._-]{0,127}\z`)

type ManagerUpdateOptions struct {
	Build  distribution.Build
	Stdout io.Writer
}

// UpdateManager promotes one already-acquired build into the independent
// manager cache. It intentionally never changes BinPath: that path belongs to
// the active native service or Docker host shim and is updated transactionally.
func UpdateManager(ctx context.Context, h Host, o ManagerUpdateOptions) error {
	if !h.IsRoot() {
		return terminalError("install.error.root")
	}
	if err := validateManagerBuild(o.Build, false); err != nil {
		return err
	}
	if _, err := inspectContract(ctx, h, []string{o.Build.BinaryPath}); err != nil {
		return err
	}
	digest, _, err := fileDigest(ctx, h, o.Build.BinaryPath, 256<<20)
	if err != nil || digest != o.Build.SHA256 {
		return terminalError("install.error.image.5")
	}
	unlock, err := h.LockLifecycle()
	if err != nil {
		return err
	}
	defer unlock()
	managerDir := path.Dir(ManagerBinaryPath)
	if err = h.MkdirAll(managerDir, 0o700); err != nil {
		return err
	}
	if err = validateManagerPath(ctx, h, managerDir, true); err != nil {
		return err
	}
	if err = h.CopyFile(o.Build.BinaryPath, ManagerBinaryPath, 0o755); err != nil {
		return err
	}
	storedDigest, _, err := fileDigest(ctx, h, ManagerBinaryPath, 256<<20)
	if err != nil || storedDigest != o.Build.SHA256 {
		return terminalError("install.error.image.5")
	}
	receipt := o.Build
	receipt.BinaryPath = ManagerBinaryPath
	if err = writeJSON(h, ManagerBuildPath, receipt); err != nil {
		return err
	}
	if o.Stdout != nil {
		fmt.Fprintf(o.Stdout, "Manager updated: %s (%s)\n", receipt.Version, receipt.Commit[:12])
	}
	return nil
}

// LoadManagerBuild verifies the receipt, root-owned cache, digest and current
// installer contract before returning an executable manager identity.
func LoadManagerBuild(ctx context.Context, h Host) (distribution.Build, error) {
	if err := validateManagerPath(ctx, h, path.Dir(ManagerBuildPath), true); err != nil {
		return distribution.Build{}, err
	}
	if err := validateManagerPath(ctx, h, ManagerBuildPath, false); err != nil {
		return distribution.Build{}, err
	}
	raw, err := readRecord(h, ManagerBuildPath)
	if err != nil {
		return distribution.Build{}, err
	}
	var b distribution.Build
	if json.Unmarshal(raw, &b) != nil || b.BinaryPath != ManagerBinaryPath {
		return distribution.Build{}, terminalError("install.error.state")
	}
	if err = validateManagerBuild(b, true); err != nil {
		return distribution.Build{}, err
	}
	if err = validateManagerPath(ctx, h, ManagerBinaryPath, false); err != nil {
		return distribution.Build{}, err
	}
	digest, _, err := fileDigest(ctx, h, ManagerBinaryPath, 256<<20)
	if err != nil || digest != b.SHA256 {
		return distribution.Build{}, terminalError("install.error.image.5")
	}
	if _, err = inspectContract(ctx, h, []string{ManagerBinaryPath}); err != nil {
		return distribution.Build{}, err
	}
	return b, nil
}

func validateManagerBuild(b distribution.Build, cached bool) error {
	if b.Channel != "release" && b.Channel != "commit" || !hexLength(b.Commit, 40) || !hexLength(b.SHA256, 64) || b.Version == "" || len(b.Version) > 160 || strings.ContainsAny(b.Version, "\r\n\t") {
		return terminalError("install.error.state")
	}
	if b.Channel == "release" && !managerRef.MatchString(b.Ref) || b.Channel == "commit" && b.Ref != b.Commit {
		return terminalError("install.error.state")
	}
	if cached {
		if b.BinaryPath != ManagerBinaryPath {
			return terminalError("install.error.state")
		}
	} else if !path.IsAbs(b.BinaryPath) || b.BinaryPath == ManagerBinaryPath {
		return terminalError("install.error.binary")
	}
	return nil
}

func validateManagerPath(ctx context.Context, h Host, name string, directory bool) error {
	info, err := h.Stat(name)
	if err != nil {
		return err
	}
	if info.IsDir() != directory || !directory && !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return terminalError("install.error.state")
	}
	if _, ok := h.(realHost); !ok {
		return nil
	}
	format := "%u %a"
	raw, err := h.Output(ctx, []string{"stat", "-c", format, name}, 10*time.Second)
	if err != nil {
		return err
	}
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) != 2 || fields[0] != "0" {
		return terminalError("install.error.state")
	}
	mode, err := strconv.ParseUint(fields[1], 8, 32)
	if err != nil || mode&0o022 != 0 {
		return terminalError("install.error.state")
	}
	return nil
}
