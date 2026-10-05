package install

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

// PrepareRuntimeImage loads a verified release image or builds an explicitly
// selected source candidate. An admitted cached image needs no registry/network.
func PrepareRuntimeImage(ctx context.Context, h Host, build *distribution.Build, b CoreBundle, parent string) (string, error) {
	if build.Channel != "release" {
		return BuildRuntimeImage(ctx, h, *build, b, parent)
	}
	if build.Runtime != nil {
		if err := CheckRuntimeManifest(*build, *build.Runtime, b); err != nil {
			return "", err
		}
		if err := inspectRuntimeImage(ctx, h, *build, *build.Runtime); err == nil {
			return build.Runtime.ImageID, nil
		}
	}
	m, archive, cleanup, err := distribution.NewClient(nil, distribution.Options{}).AcquireRuntime(ctx, *build, parent)
	defer cleanup()
	if err != nil {
		return "", err
	}
	if err := LoadRuntimeImage(ctx, h, *build, m, b, archive); err != nil {
		return "", err
	}
	build.Runtime = &m
	return m.ImageID, nil
}

func CheckRuntimeManifest(build distribution.Build, m distribution.RuntimeManifest, b CoreBundle) error {
	if err := m.Validate(build); err != nil {
		return err
	}
	selected, err := SelectCore(b.ID)
	if err != nil || selected != b || m.DataContract != CurrentContract().DataContract ||
		m.DeploymentSchema != StateSchema || m.MaintenanceProtocol != CurrentContract().MaintenanceProtocol ||
		m.ToolsVersion != b.ToolsVersion || m.ToolsCommit != b.ToolsCommit || m.UserspaceVersion != b.UserspaceVersion || m.UserspaceCommit != b.UserspaceCommit {
		return fmt.Errorf("runtime: release does not support the selected data, deployment or reviewed core contract")
	}
	for _, k := range m.Kernels {
		if k.ID == b.ID && k.Version == b.KernelVersion && k.Commit == b.KernelCommit {
			return nil
		}
	}
	return fmt.Errorf("runtime: selected reviewed kernel is absent from release metadata")
}

// LoadRuntimeImage also serves explicit offline imports. Its metadata must be
// obtained with the independently verified manager/release checksums first.
// Hashing precedes docker load; labels and platform are rechecked afterwards.
func LoadRuntimeImage(ctx context.Context, h Host, build distribution.Build, m distribution.RuntimeManifest, b CoreBundle, archive string) error {
	if err := CheckRuntimeManifest(build, m, b); err != nil {
		return err
	}
	info, err := h.Stat(archive)
	if err != nil || !info.Mode().IsRegular() || info.Size() != m.ArchiveSize {
		return fmt.Errorf("runtime: image archive size or type is invalid")
	}
	digest, _, err := fileDigest(ctx, h, archive, distribution.RuntimeArchiveLimit)
	if err != nil || digest != m.ArchiveSHA256 {
		return fmt.Errorf("runtime: image archive checksum failed")
	}
	if err := runQuiet(ctx, h, []string{"docker", "load", "--input", archive}, longTimeout); err != nil {
		return err
	}
	return inspectRuntimeImage(ctx, h, build, m)
}

func inspectRuntimeImage(ctx context.Context, h Host, build distribution.Build, m distribution.RuntimeManifest) error {
	raw, err := h.Output(ctx, []string{"docker", "image", "inspect", "--format", "{{.Os}}/{{.Architecture}} {{.Id}} {{json .Config.Labels}}", m.ImageID}, 30*time.Second)
	if err != nil || len(raw) > 16<<10 {
		return fmt.Errorf("runtime: image inspection failed")
	}
	parts := strings.SplitN(strings.TrimSpace(raw), " ", 3)
	if len(parts) != 3 || parts[0] != "linux/amd64" || parts[1] != m.ImageID {
		return fmt.Errorf("runtime: loaded image platform or identity mismatch")
	}
	var labels map[string]string
	if json.Unmarshal([]byte(parts[2]), &labels) != nil {
		return fmt.Errorf("runtime: malformed image provenance")
	}
	expected := runtimeIdentityLabels(build, m)
	for name, value := range expected {
		if labels[name] != value {
			return fmt.Errorf("runtime: image provenance mismatch")
		}
	}
	return nil
}

func runtimeIdentityLabels(build distribution.Build, m distribution.RuntimeManifest) map[string]string {
	return map[string]string{
		"org.opencontainers.image.revision": build.Commit,
		"io.wg-guard.binary.sha256":         build.SHA256,
		"io.wg-guard.runtime.recipe.sha256": m.RecipeSHA256,
		"io.wg-guard.deployment.schema":     strconv.Itoa(m.DeploymentSchema),
		"io.wg-guard.data.contract":         m.DataContract,
		"io.wg-guard.maintenance.protocol":  strconv.Itoa(m.MaintenanceProtocol),
		"io.wg-guard.awg-tools.commit":      m.ToolsCommit,
		"io.wg-guard.awg-userspace.commit":  m.UserspaceCommit,
		"io.wg-guard.notices.sha256":        m.NoticesSHA256,
	}
}
