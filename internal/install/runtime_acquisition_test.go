package install

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

func runtimeFixture(t *testing.T) (*memHost, distribution.Build, distribution.RuntimeManifest, CoreBundle) {
	t.Helper()
	h := newMemHost()
	b, _ := SelectCore("recommended")
	body := []byte("synthetic runtime archive")
	build := distribution.Build{Channel: "release", Ref: "v1", Version: "v1", Commit: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)}
	m := distribution.RuntimeManifest{Schema: 1, Version: build.Version, Commit: build.Commit, Platform: "linux/amd64", BinarySHA256: build.SHA256, ImageID: "sha256:" + strings.Repeat("c", 64), RecipeSHA256: RuntimeRecipeSHA256(), Archive: distribution.RuntimeArchiveName, ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(body)), ArchiveSize: int64(len(body)), DataContract: CurrentContract().DataContract, DeploymentSchema: StateSchema, MaintenanceProtocol: 2, ToolsVersion: b.ToolsVersion, ToolsCommit: b.ToolsCommit, UserspaceVersion: b.UserspaceVersion, UserspaceCommit: b.UserspaceCommit, Kernels: []distribution.KernelIdentity{{ID: b.ID, Version: b.KernelVersion, Commit: b.KernelCommit}}, SBOMSHA256: strings.Repeat("d", 64), NoticesSHA256: RuntimeNoticesSHA256()}
	labels := runtimeIdentityLabels(build, m)
	raw, _ := json.Marshal(labels)
	h.output["docker image inspect --format {{.Os}}/{{.Architecture}} {{.Id}} {{json .Config.Labels}} "+m.ImageID] = "linux/amd64 " + m.ImageID + " " + string(raw)
	h.files["/tmp/runtime.tar.gz"] = memFile{data: body, perm: 0600}
	return h, build, m, b
}

func TestRuntimeImportVerifiesBeforeLoadAndNeverActivatesService(t *testing.T) {
	for _, kind := range []string{"valid", "bad-archive", "wrong-binary", "wrong-contract", "wrong-core", "wrong-platform", "wrong-label"} {
		t.Run(kind, func(t *testing.T) {
			h, build, m, b := runtimeFixture(t)
			switch kind {
			case "bad-archive":
				h.files["/tmp/runtime.tar.gz"] = memFile{data: []byte("synthetic altered archive"), perm: 0600}
			case "wrong-binary":
				m.BinarySHA256 = strings.Repeat("e", 64)
			case "wrong-contract":
				m.DeploymentSchema = 3
			case "wrong-core":
				m.ToolsCommit = strings.Repeat("f", 40)
			case "wrong-platform":
				m.Platform = "linux/arm64"
			case "wrong-label":
				h.output["docker image inspect --format {{.Os}}/{{.Architecture}} {{.Id}} {{json .Config.Labels}} "+m.ImageID] = "linux/amd64 " + m.ImageID + " {}"
			}
			err := LoadRuntimeImage(context.Background(), h, build, m, b, "/tmp/runtime.tar.gz")
			if (err == nil) != (kind == "valid") {
				t.Fatal("runtime import outcome", kind, err)
			}
			if kind != "valid" && kind != "wrong-label" && h.ran("docker", "load") {
				t.Fatal("unverified archive reached Docker")
			}
			if h.ran("docker", "compose") || h.ran("systemctl") {
				t.Fatal("artifact import activated a deployment")
			}
		})
	}
}

func TestCachedVerifiedRuntimeNeedsNoRegistryOrImageBuild(t *testing.T) {
	h, build, m, b := runtimeFixture(t)
	build.Runtime = &m
	id, err := PrepareRuntimeImage(context.Background(), h, &build, b, "")
	if err != nil || id != m.ImageID || h.ran("docker", "build") || h.ran("docker", "load") || h.ran("docker", "pull") {
		t.Fatal("offline image reuse failed", err)
	}
}
