package distribution

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const RuntimeArchiveName = "runtime_linux_amd64.tar.gz"
const RuntimeArchiveLimit int64 = 1 << 30

type KernelIdentity struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// ImageID is Docker's immutable config digest, not a registry manifest digest.
// The archive and publisher channel are verified separately from that identity.
// Docker's containerd image store names the loaded image by the archive's OCI
// manifest digest instead; InspectRuntimeArchive derives it from the verified
// archive. Metadata gains no field for it: existing managers decode this
// schema strictly and would refuse unknown keys.
type RuntimeManifest struct {
	Schema              int              `json:"schema"`
	Version             string           `json:"version"`
	Commit              string           `json:"commit"`
	Platform            string           `json:"platform"`
	BinarySHA256        string           `json:"binary_sha256"`
	ImageID             string           `json:"image_id"`
	RecipeSHA256        string           `json:"recipe_sha256"`
	Archive             string           `json:"archive"`
	ArchiveSHA256       string           `json:"archive_sha256"`
	ArchiveSize         int64            `json:"archive_size"`
	DataContract        string           `json:"data_contract"`
	DeploymentSchema    int              `json:"deployment_schema"`
	MaintenanceProtocol int              `json:"maintenance_protocol"`
	DomainProtocol      int              `json:"domain_protocol,omitempty"`
	ToolsVersion        string           `json:"tools_version"`
	ToolsCommit         string           `json:"tools_commit"`
	UserspaceVersion    string           `json:"userspace_version"`
	UserspaceCommit     string           `json:"userspace_commit"`
	Kernels             []KernelIdentity `json:"kernels"`
	SBOMSHA256          string           `json:"sbom_sha256"`
	NoticesSHA256       string           `json:"notices_sha256"`
}

func (m RuntimeManifest) Validate(b Build) error {
	if m.Schema != 1 || !safeRef.MatchString(m.Version) || !commitSHA.MatchString(b.Commit) || !digestSHA.MatchString(b.SHA256) || m.Version != b.Version || m.Commit != b.Commit || m.BinarySHA256 != b.SHA256 ||
		m.Platform != "linux/amd64" || m.Archive != RuntimeArchiveName || m.ArchiveSize <= 0 || m.ArchiveSize > RuntimeArchiveLimit ||
		!imageDigest(m.ImageID) ||
		!digestSHA.MatchString(m.ArchiveSHA256) || !digestSHA.MatchString(m.RecipeSHA256) ||
		!digestSHA.MatchString(m.SBOMSHA256) || !digestSHA.MatchString(m.NoticesSHA256) ||
		!commitSHA.MatchString(m.ToolsCommit) || !commitSHA.MatchString(m.UserspaceCommit) || len(m.Kernels) < 1 || len(m.Kernels) > 8 {
		return fmt.Errorf("distribution: runtime identity does not match the verified manager")
	}
	seen := map[string]bool{}
	for _, k := range m.Kernels {
		if !safeRef.MatchString(k.ID) || !safeRef.MatchString(k.Version) || !commitSHA.MatchString(k.Commit) || seen[k.ID] {
			return fmt.Errorf("distribution: invalid reviewed kernel inventory")
		}
		seen[k.ID] = true
	}
	return nil
}

// AcquireRuntime requires new-format release assets. Older binary-only releases
// are refused; a missing image never falls back to compiling on the production host.
// All temporary files are isolated, bounded and removed on failure.
func (c *Client) AcquireRuntime(ctx context.Context, b Build, parent string) (manifest RuntimeManifest, archive string, cleanup func(), err error) {
	cleanup = func() {}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	if b.Channel != "release" || b.Ref != b.Version || !safeRef.MatchString(b.Ref) || !filepath.IsAbs(parent) {
		return manifest, "", cleanup, fmt.Errorf("distribution: runtime requires an exact verified release and private staging directory")
	}
	dir, err := os.MkdirTemp(parent, "wg-guard-image-")
	if err != nil {
		return manifest, "", cleanup, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	r, err := c.release(ctx, b.Ref)
	if err != nil {
		return manifest, "", cleanup, err
	}
	checks, err := c.asset(r, "checksums.txt", 64<<10)
	if err != nil {
		return manifest, "", cleanup, err
	}
	sums := filepath.Join(dir, "checksums.txt")
	if _, err = c.download(ctx, checks.URL, sums, 64<<10, checks.Size); err != nil {
		return manifest, "", cleanup, err
	}
	content, err := os.ReadFile(sums)
	if err != nil {
		return manifest, "", cleanup, err
	}
	meta, err := c.asset(r, "runtime-metadata.json", 64<<10)
	if err != nil {
		return manifest, "", cleanup, err
	}
	expected, err := checksumFor(string(content), meta.Name)
	if err != nil {
		return manifest, "", cleanup, err
	}
	metaPath := filepath.Join(dir, meta.Name)
	digest, err := c.download(ctx, meta.URL, metaPath, 64<<10, meta.Size)
	if err != nil {
		return manifest, "", cleanup, err
	}
	if digest != expected {
		return manifest, "", cleanup, fmt.Errorf("distribution: runtime metadata checksum failed")
	}
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return manifest, "", cleanup, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&manifest); err != nil {
		return manifest, "", cleanup, fmt.Errorf("distribution: malformed runtime metadata")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return manifest, "", cleanup, fmt.Errorf("distribution: trailing runtime metadata")
	}
	if err = manifest.Validate(b); err != nil {
		return manifest, "", cleanup, err
	}
	if expectedSBOM, e := checksumFor(string(content), "sbom.spdx.json"); e != nil || expectedSBOM != manifest.SBOMSHA256 {
		return manifest, "", cleanup, fmt.Errorf("distribution: runtime SBOM identity mismatch")
	}
	asset, err := c.asset(r, RuntimeArchiveName, RuntimeArchiveLimit)
	if err != nil {
		return manifest, "", cleanup, err
	}
	expected, err = checksumFor(string(content), asset.Name)
	if err != nil || expected != manifest.ArchiveSHA256 || asset.Size != manifest.ArchiveSize {
		return manifest, "", cleanup, fmt.Errorf("distribution: runtime archive identity is inconsistent")
	}
	archive = filepath.Join(dir, asset.Name)
	c.progress("Downloading verified Docker runtime")
	digest, err = c.download(ctx, asset.URL, archive, RuntimeArchiveLimit, asset.Size)
	if err != nil {
		return manifest, "", cleanup, err
	}
	if digest != expected {
		return manifest, "", cleanup, fmt.Errorf("distribution: runtime archive checksum failed")
	}
	return manifest, archive, cleanup, nil
}
