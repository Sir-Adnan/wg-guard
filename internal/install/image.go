package install

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	legal "github.com/Sir-Adnan/wg-guard"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Sir-Adnan/wg-guard/internal/distribution"
)

// BuildRuntimeImage consumes an acquired artifact without executing it. The
// caller owns stagingParent; only our random private child is ever removed.
// A successful return is an immutable local Docker image ID, never a tag.
// M3 owns selecting this image for install/update and recording it in State.
func BuildRuntimeImage(ctx context.Context, h Host, build distribution.Build, b CoreBundle, stagingParent string) (string, error) {
	selected, err := SelectCore(b.ID)
	if err != nil || selected != b {
		return "", terminalError("install.error.image.1")
	}
	if !filepath.IsAbs(stagingParent) || !filepath.IsAbs(build.BinaryPath) || !hexLength(build.SHA256, 64) || !hexLength(build.Commit, 40) {
		return "", terminalError("install.error.image.2")
	}
	info, err := os.Stat(stagingParent)
	if err != nil || !info.IsDir() {
		return "", terminalError("install.error.image.3")
	}
	dir, err := os.MkdirTemp(stagingParent, "wg-guard-runtime-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	source, err := os.Open(build.BinaryPath)
	if err != nil {
		return "", err
	}
	defer source.Close()
	info, err = source.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return "", terminalError("install.error.image.4")
	}
	target, err := os.OpenFile(filepath.Join(dir, "wg-guard"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	digest := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(target, digest), io.LimitReader(source, 256<<20+1))
	closeErr := target.Close()
	if copyErr != nil || closeErr != nil || n > 256<<20 || hex.EncodeToString(digest.Sum(nil)) != build.SHA256 {
		return "", terminalError("install.error.image.5")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := WriteRuntimeContext(dir, b); err != nil {
		return "", err
	}
	iid := filepath.Join(dir, "image-id")
	c := CurrentContract()
	labels := runtimeIdentityLabels(build, distribution.RuntimeManifest{RecipeSHA256: RuntimeRecipeSHA256(), NoticesSHA256: RuntimeNoticesSHA256(), DataContract: c.DataContract, DeploymentSchema: c.DeploymentSchema, MaintenanceProtocol: c.MaintenanceProtocol, DomainProtocol: c.DomainProtocol, ToolsCommit: b.ToolsCommit, UserspaceCommit: b.UserspaceCommit})
	labels["io.wg-guard.core.bundle"] = b.ID
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := []string{"docker", "build", "--platform", "linux/amd64", "--iidfile", iid}
	for _, key := range keys {
		args = append(args, "--label", key+"="+labels[key])
	}
	args = append(args, dir)
	if err := runQuiet(ctx, h, args, longTimeout); err != nil {
		return "", terminalError("install.error.image.6", err)
	}
	file, err := os.Open(iid)
	if err != nil {
		return "", terminalError("install.error.image.7")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 128))
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(data))
	if !strings.HasPrefix(id, "sha256:") || !hexLength(strings.TrimPrefix(id, "sha256:"), 64) {
		return "", terminalError("install.error.image.8")
	}
	return id, nil
}

func hexLength(s string, n int) bool {
	if len(s) != n || s != strings.ToLower(s) {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

//go:embed runtime/Dockerfile
var runtimeRecipe string

// RuntimeDockerfile is the only runtime recipe used by acquisition and CI.
// Callers must select a reviewed bundle before rendering.
func RuntimeDockerfile(b CoreBundle) string {
	return strings.NewReplacer("{{TOOLS_VERSION}}", b.ToolsVersion, "{{TOOLS_REPOSITORY}}", b.ToolsRepository, "{{TOOLS_COMMIT}}", b.ToolsCommit, "{{USERSPACE_VERSION}}", b.UserspaceVersion, "{{USERSPACE_COMMIT}}", b.UserspaceCommit).Replace(runtimeRecipe)
}

func RuntimeRecipeSHA256() string {
	sum := sha256.Sum256([]byte(runtimeRecipe))
	return hex.EncodeToString(sum[:])
}

func RuntimeNoticesSHA256() string {
	h := sha256.New()
	_ = fs.WalkDir(legal.Notices, ".", func(name string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			data, _ := legal.Notices.ReadFile(name)
			_, _ = fmt.Fprintf(h, "%s\x00%d\x00", name, len(data))
			_, _ = h.Write(data)
		}
		return err
	})
	return hex.EncodeToString(h.Sum(nil))
}

// RuntimeBuildInfo is a data-free build probe for CI and offline packaging.
// Its legal files and recipe come from this exact binary, not the working tree.
func RuntimeBuildInfo(selector string) (any, error) {
	b, err := SelectCore(selector)
	if err != nil {
		return nil, err
	}
	notices := map[string]string{}
	if err := fs.WalkDir(legal.Notices, ".", func(name string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			data, e := legal.Notices.ReadFile(name)
			if e != nil {
				return e
			}
			notices[name] = string(data)
		}
		return err
	}); err != nil {
		return nil, err
	}
	return struct {
		Dockerfile    string            `json:"dockerfile"`
		RecipeSHA256  string            `json:"recipe_sha256"`
		NoticesSHA256 string            `json:"notices_sha256"`
		Contract      Contract          `json:"contract"`
		Core          CoreBundle        `json:"core"`
		Kernels       []CoreBundle      `json:"kernels"`
		Notices       map[string]string `json:"notices"`
	}{RuntimeDockerfile(b), RuntimeRecipeSHA256(), RuntimeNoticesSHA256(), CurrentContract(), b, ReviewedCoreBundles(), notices}, nil
}

// WriteRuntimeContext writes into a newly owned private staging directory.
// Fixed embedded paths cannot read live configuration or private keys.
func WriteRuntimeContext(dir string, b CoreBundle) error {
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(RuntimeDockerfile(b)), 0600); err != nil {
		return err
	}
	return fs.WalkDir(legal.Notices, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == "." {
			return err
		}
		target := filepath.Join(dir, "notices", filepath.FromSlash(name))
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := legal.Notices.ReadFile(name)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
}
