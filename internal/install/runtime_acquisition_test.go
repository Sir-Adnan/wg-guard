package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

func runtimeFixture(t *testing.T) (*memHost, distribution.Build, distribution.RuntimeManifest, CoreBundle) {
	t.Helper()
	h := newMemHost()
	b, _ := SelectCore("recommended")
	config := "sha256:" + strings.Repeat("c", 64)
	body, _ := ociRuntimeArchive(config)
	build := distribution.Build{Channel: "release", Ref: "v1", Version: "v1", Commit: strings.Repeat("a", 40), SHA256: strings.Repeat("b", 64)}
	m := distribution.RuntimeManifest{Schema: 1, Version: build.Version, Commit: build.Commit, Platform: "linux/amd64", BinarySHA256: build.SHA256, ImageID: config, RecipeSHA256: RuntimeRecipeSHA256(), Archive: distribution.RuntimeArchiveName, ArchiveSHA256: fmt.Sprintf("%x", sha256.Sum256(body)), ArchiveSize: int64(len(body)), DataContract: CurrentContract().DataContract, DeploymentSchema: StateSchema, MaintenanceProtocol: 2, DomainProtocol: CurrentContract().DomainProtocol, ToolsVersion: b.ToolsVersion, ToolsCommit: b.ToolsCommit, UserspaceVersion: b.UserspaceVersion, UserspaceCommit: b.UserspaceCommit, Kernels: []distribution.KernelIdentity{{ID: b.ID, Version: b.KernelVersion, Commit: b.KernelCommit}}, SBOMSHA256: strings.Repeat("d", 64), NoticesSHA256: RuntimeNoticesSHA256()}
	labels := runtimeIdentityLabels(build, m)
	raw, _ := json.Marshal(labels)
	h.output["docker image inspect --format {{.Os}}/{{.Architecture}} {{.Id}} {{json .Config.Labels}} "+m.ImageID] = "linux/amd64 " + m.ImageID + " " + string(raw)
	h.files["/tmp/runtime.tar.gz"] = memFile{data: body, perm: 0600}
	return h, build, m, b
}

func TestRuntimeImportVerifiesBeforeLoadAndNeverActivatesService(t *testing.T) {
	for _, kind := range []string{"valid", "bad-archive", "wrong-binary", "wrong-contract", "wrong-core", "wrong-platform", "wrong-label", "foreign-config"} {
		t.Run(kind, func(t *testing.T) {
			h, build, m, b := runtimeFixture(t)
			switch kind {
			case "bad-archive":
				altered := append([]byte(nil), h.files["/tmp/runtime.tar.gz"].data...)
				altered[len(altered)-1] ^= 1
				h.files["/tmp/runtime.tar.gz"] = memFile{data: altered, perm: 0600}
			case "wrong-binary":
				m.BinarySHA256 = strings.Repeat("e", 64)
			case "wrong-contract":
				m.DeploymentSchema = 3
			case "wrong-core":
				m.ToolsCommit = strings.Repeat("f", 40)
			case "wrong-platform":
				m.Platform = "linux/arm64"
			case "foreign-config":
				other, _ := ociRuntimeArchive("sha256:" + strings.Repeat("e", 64))
				h.files["/tmp/runtime.tar.gz"] = memFile{data: other, perm: 0600}
				m.ArchiveSHA256, m.ArchiveSize = fmt.Sprintf("%x", sha256.Sum256(other)), int64(len(other))
			case "wrong-label":
				h.output["docker image inspect --format {{.Os}}/{{.Architecture}} {{.Id}} {{json .Config.Labels}} "+m.ImageID] = "linux/amd64 " + m.ImageID + " {}"
			}
			_, err := LoadRuntimeImage(context.Background(), h, build, m, b, "/tmp/runtime.tar.gz")
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

// containerdStoreHost answers like Docker's containerd image store: the image is
// known only by its OCI manifest digest, never by the config digest.
type containerdStoreHost struct {
	*memHost
	config, manifest, labels string
}

func (h *containerdStoreHost) Output(ctx context.Context, argv []string, timeout time.Duration) (string, error) {
	if len(argv) > 3 && argv[0] == "docker" && argv[1] == "image" && argv[2] == "inspect" {
		h.commands = append(h.commands, memCmd{argv: argv})
		if argv[len(argv)-1] == h.manifest {
			return "linux/amd64 " + h.manifest + " " + h.labels, nil
		}
		return "", fmt.Errorf("Error response from daemon: No such image: %s", argv[len(argv)-1])
	}
	return h.memHost.Output(ctx, argv, timeout)
}

func TestRuntimeImageAdmissionSupportsContainerdImageStore(t *testing.T) {
	mem, build, m, b := runtimeFixture(t)
	_, manifest := ociRuntimeArchive(m.ImageID)
	raw, _ := json.Marshal(runtimeIdentityLabels(build, m))
	h := &containerdStoreHost{memHost: mem, config: m.ImageID, manifest: manifest, labels: string(raw)}

	id, err := LoadRuntimeImage(context.Background(), h, build, m, b, "/tmp/runtime.tar.gz")
	if err != nil || id != manifest {
		t.Fatalf("containerd-store load id=%s err=%v", id, err)
	}

	// Without the archive only the config digest is knowable, so a cached
	// containerd-store image is re-acquired rather than trusted by labels.
	if _, err := admitRuntimeImage(context.Background(), h, build, m, ""); err == nil {
		t.Fatal("containerd-store image admitted without its archive-derived identity")
	}

	h.labels = "{}"
	if _, err := admitRuntimeImage(context.Background(), h, build, m, manifest); err == nil {
		t.Fatal("containerd-store image admitted without provenance labels")
	}
}

// ociRuntimeArchive builds a minimal gzip `docker save` export: an OCI index
// with one manifest blob referencing config, plus the legacy manifest.json.
func ociRuntimeArchive(config string) ([]byte, string) {
	manifest := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"mediaType":"application/vnd.oci.image.config.v1+json","digest":"` + config + `","size":2},"layers":[]}`)
	manifestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(manifest))
	index := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"` + manifestDigest + `","size":` + fmt.Sprint(len(manifest)) + `}]}`)
	legacy := []byte(`[{"Config":"blobs/sha256/` + strings.TrimPrefix(config, "sha256:") + `","RepoTags":null,"Layers":[]}]`)
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name string
		body []byte
	}{{"blobs/sha256/" + strings.TrimPrefix(manifestDigest, "sha256:"), manifest}, {"index.json", index}, {"manifest.json", legacy}, {"oci-layout", []byte(`{"imageLayoutVersion":"1.0.0"}`)}} {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0644, Size: int64(len(f.body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(f.body)
	}
	_ = tw.Close()
	_ = gz.Close()
	return out.Bytes(), manifestDigest
}
