package install

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

// PrepareRuntimeImage loads a verified release image or builds an explicitly
// selected source candidate. An admitted cached image needs no registry/network.
// It returns the image ID the local Docker image store uses.
func PrepareRuntimeImage(ctx context.Context, h Host, build *distribution.Build, b CoreBundle, parent string) (string, error) {
	if build.Channel != "release" {
		return BuildRuntimeImage(ctx, h, *build, b, parent)
	}
	if build.Runtime != nil {
		if err := CheckRuntimeManifest(*build, *build.Runtime, b); err != nil {
			return "", err
		}
		// Only the classic store's config-digest ID is knowable without the
		// archive; a containerd-store host re-acquires the verified archive.
		if id, err := admitRuntimeImage(ctx, h, *build, *build.Runtime, ""); err == nil {
			return id, nil
		}
	}
	m, archive, cleanup, err := distribution.NewClient(nil, distribution.Options{}).AcquireRuntime(ctx, *build, parent)
	defer cleanup()
	if err != nil {
		return "", err
	}
	id, err := LoadRuntimeImage(ctx, h, *build, m, b, archive)
	if err != nil {
		return "", err
	}
	build.Runtime = &m
	return id, nil
}

func CheckRuntimeManifest(build distribution.Build, m distribution.RuntimeManifest, b CoreBundle) error {
	if err := m.Validate(build); err != nil {
		return err
	}
	selected, err := SelectCore(b.ID)
	if err != nil || selected != b || m.DataContract != CurrentContract().DataContract ||
		m.DeploymentSchema != StateSchema || m.MaintenanceProtocol != CurrentContract().MaintenanceProtocol || m.DomainProtocol != CurrentContract().DomainProtocol ||
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
// Hashing and identity parsing precede docker load in one pass over the same
// bytes; platform, identity and labels are rechecked afterwards. It returns the
// image ID the local Docker image store assigned.
func LoadRuntimeImage(ctx context.Context, h Host, build distribution.Build, m distribution.RuntimeManifest, b CoreBundle, archive string) (string, error) {
	if err := CheckRuntimeManifest(build, m, b); err != nil {
		return "", err
	}
	info, err := h.Stat(archive)
	if err != nil || !info.Mode().IsRegular() || info.Size() != m.ArchiveSize {
		return "", fmt.Errorf("runtime: image archive size or type is invalid")
	}
	identity, err := inspectRuntimeArchive(ctx, h, archive)
	if err != nil || identity.ArchiveSHA256 != m.ArchiveSHA256 {
		return "", fmt.Errorf("runtime: image archive checksum failed")
	}
	if identity.ConfigDigest != m.ImageID {
		return "", fmt.Errorf("runtime: image archive identity mismatch")
	}
	if err := runQuiet(ctx, h, []string{"docker", "load", "--input", archive}, longTimeout); err != nil {
		return "", err
	}
	return admitRuntimeImage(ctx, h, build, m, identity.ManifestDigest)
}

func inspectRuntimeArchive(ctx context.Context, h Host, archive string) (distribution.RuntimeArchiveIdentity, error) {
	if _, ok := h.(realHost); ok {
		if err := safeHostPath(archive); err != nil {
			return distribution.RuntimeArchiveIdentity{}, err
		}
	}
	f, err := h.Open(archive)
	if err != nil {
		return distribution.RuntimeArchiveIdentity{}, err
	}
	defer f.Close()
	return distribution.InspectRuntimeArchive(contextReader{ctx: ctx, r: f}, distribution.RuntimeArchiveLimit)
}

// admitRuntimeImage finds the loaded image under either Docker identity: the
// classic store uses the config digest, the containerd store the OCI manifest
// digest. Platform and provenance labels must match whichever store answers.
func admitRuntimeImage(ctx context.Context, h Host, build distribution.Build, m distribution.RuntimeManifest, manifestDigest string) (string, error) {
	accepted := map[string]bool{m.ImageID: true}
	refs := []string{m.ImageID}
	if manifestDigest != "" && manifestDigest != m.ImageID {
		accepted[manifestDigest] = true
		refs = append(refs, manifestDigest)
	}
	for _, ref := range refs {
		raw, err := h.Output(ctx, []string{"docker", "image", "inspect", "--format", "{{.Os}}/{{.Architecture}} {{.Id}} {{json .Config.Labels}}", ref}, 30*time.Second)
		if err != nil || len(raw) > 16<<10 {
			continue
		}
		parts := strings.SplitN(strings.TrimSpace(raw), " ", 3)
		if len(parts) != 3 || parts[0] != "linux/amd64" || !accepted[parts[1]] {
			return "", fmt.Errorf("runtime: loaded image platform or identity mismatch")
		}
		var labels map[string]string
		if json.Unmarshal([]byte(parts[2]), &labels) != nil {
			return "", fmt.Errorf("runtime: malformed image provenance")
		}
		for name, value := range runtimeIdentityLabels(build, m) {
			if labels[name] != value {
				return "", fmt.Errorf("runtime: image provenance mismatch")
			}
		}
		return parts[1], nil
	}
	return "", fmt.Errorf("runtime: image inspection failed")
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (c contextReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

func runtimeIdentityLabels(build distribution.Build, m distribution.RuntimeManifest) map[string]string {
	return map[string]string{
		"org.opencontainers.image.revision": build.Commit,
		"io.wg-guard.binary.sha256":         build.SHA256,
		"io.wg-guard.runtime.recipe.sha256": m.RecipeSHA256,
		"io.wg-guard.deployment.schema":     strconv.Itoa(m.DeploymentSchema),
		"io.wg-guard.data.contract":         m.DataContract,
		"io.wg-guard.maintenance.protocol":  strconv.Itoa(m.MaintenanceProtocol),
		"io.wg-guard.domain.protocol":       strconv.Itoa(m.DomainProtocol),
		"io.wg-guard.awg-tools.commit":      m.ToolsCommit,
		"io.wg-guard.awg-userspace.commit":  m.UserspaceCommit,
		"io.wg-guard.notices.sha256":        m.NoticesSHA256,
	}
}
